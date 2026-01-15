package validators

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// ValidationStatus represents the current status of a validation
type ValidationStatus string

const (
	StatusActive    ValidationStatus = "active"
	StatusPending   ValidationStatus = "pending"
	StatusCompleted ValidationStatus = "completed"
	StatusRemoved   ValidationStatus = "removed"
)

// BlsCredentials contains BLS key information for permissionless validators
type BlsCredentials struct {
	PublicKey         string `json:"publicKey"`
	ProofOfPossession string `json:"proofOfPossession"`
}

// Rewards contains reward information for completed validators
type Rewards struct {
	ValidationRewardAmount string   `json:"validationRewardAmount"`
	DelegationRewardAmount string   `json:"delegationRewardAmount"`
	RewardAddresses        []string `json:"rewardAddresses,omitempty"`
	RewardTxHash           string   `json:"rewardTxHash,omitempty"`
}

// ValidatorRecord stores all validation data indexed by registration tx
type ValidatorRecord struct {
	TxHash         string           `json:"txHash"`
	NodeID         string           `json:"nodeId"`
	SubnetID       string           `json:"subnetId"`
	AmountStaked   string           `json:"amountStaked"`
	DelegationFee  string           `json:"delegationFee,omitempty"`
	StartTimestamp int64            `json:"startTimestamp"`
	EndTimestamp   int64            `json:"endTimestamp"`
	BlsCredentials *BlsCredentials  `json:"blsCredentials,omitempty"`
	RewardAddrs    []string         `json:"rewardAddresses,omitempty"` // For potential rewards lookup
	BlockHeight    uint64           `json:"blockHeight"`               // Registration block

	// Completed/Removed fields
	RewardTxHash    string `json:"rewardTxHash,omitempty"`
	RemoveTxHash    string `json:"removeTxHash,omitempty"`
	RemoveTimestamp int64  `json:"removeTimestamp,omitempty"`

	// Aggregated fields (for active/completed)
	DelegatorCount  int    `json:"delegatorCount"`
	AmountDelegated string `json:"amountDelegated,omitempty"`

	// Reward amounts (populated after completion)
	ValidationRewardAmount string `json:"validationRewardAmount,omitempty"`
	DelegationRewardAmount string `json:"delegationRewardAmount,omitempty"`
}

// DelegatorRecord stores delegator information
type DelegatorRecord struct {
	TxHash         string   `json:"txHash"`
	NodeID         string   `json:"nodeId"`
	SubnetID       string   `json:"subnetId"`
	AmountDelegated string  `json:"amountDelegated"`
	StartTimestamp int64    `json:"startTimestamp"`
	EndTimestamp   int64    `json:"endTimestamp"`
	RewardAddrs    []string `json:"rewardAddresses,omitempty"`
	BlockHeight    uint64   `json:"blockHeight"`
	Completed      bool     `json:"completed"`
	RewardTxHash   string   `json:"rewardTxHash,omitempty"`
}

// ComputeStatus calculates the validation status based on timestamps
func (r *ValidatorRecord) ComputeStatus() ValidationStatus {
	if r.RemoveTxHash != "" {
		return StatusRemoved
	}
	if r.RewardTxHash != "" {
		return StatusCompleted
	}
	now := time.Now().Unix()
	if now < r.StartTimestamp {
		return StatusPending
	}
	if now < r.EndTimestamp {
		return StatusActive
	}
	// Past end time but no reward tx yet - still "active" until processed
	// Or if end time passed naturally, we consider it completed
	return StatusCompleted
}

// ToAPIResponse converts the record to the appropriate API response format
func (r *ValidatorRecord) ToAPIResponse() map[string]any {
	status := r.ComputeStatus()
	resp := map[string]any{
		"txHash":           r.TxHash,
		"nodeId":           r.NodeID,
		"subnetId":         r.SubnetID,
		"amountStaked":     r.AmountStaked,
		"startTimestamp":   r.StartTimestamp,
		"endTimestamp":     r.EndTimestamp,
		"validationStatus": string(status),
	}

	if r.DelegationFee != "" {
		resp["delegationFee"] = r.DelegationFee
	}

	if r.BlsCredentials != nil {
		resp["blsCredentials"] = r.BlsCredentials
	}

	switch status {
	case StatusActive:
		resp["delegatorCount"] = r.DelegatorCount
		if r.AmountDelegated != "" {
			resp["amountDelegated"] = r.AmountDelegated
		}
		// potentialRewards would need reward calculation - skip for now
		if len(r.RewardAddrs) > 0 {
			resp["potentialRewards"] = map[string]any{
				"validationRewardAmount": "0",
				"delegationRewardAmount": "0",
				"rewardAddresses":        r.RewardAddrs,
			}
		}

	case StatusCompleted:
		resp["delegatorCount"] = r.DelegatorCount
		if r.AmountDelegated != "" {
			resp["amountDelegated"] = r.AmountDelegated
		}
		rewards := map[string]any{
			"validationRewardAmount": r.ValidationRewardAmount,
			"delegationRewardAmount": r.DelegationRewardAmount,
		}
		if len(r.RewardAddrs) > 0 {
			rewards["rewardAddresses"] = r.RewardAddrs
		}
		if r.RewardTxHash != "" {
			rewards["rewardTxHash"] = r.RewardTxHash
		}
		resp["rewards"] = rewards

	case StatusRemoved:
		resp["removeTxHash"] = r.RemoveTxHash
		resp["removeTimestamp"] = r.RemoveTimestamp

	case StatusPending:
		// Minimal fields only
	}

	return resp
}

// Storage keys:
// v:{txHash}                       -> ValidatorRecord (main record)
// vn:{nodeId}:{startTs}:{txHash}   -> "" (index: validators by nodeId, sorted by start time desc)
// vs:{subnetId}:{startTs}:{txHash} -> "" (index: by subnet, sorted by start time desc)
// d:{txHash}                       -> DelegatorRecord
// dn:{validatorTxHash}:{txHash}    -> "" (index: delegators by validator)

func (v *Validators) saveValidator(batch *pebble.Batch, rec *ValidatorRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	// Primary key
	key := []byte(fmt.Sprintf("v:%s", rec.TxHash))
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return err
	}

	// Index by nodeId (padded start timestamp for sorting)
	nodeKey := []byte(fmt.Sprintf("vn:%s:%020d:%s", rec.NodeID, rec.StartTimestamp, rec.TxHash))
	if err := batch.Set(nodeKey, []byte{}, pebble.Sync); err != nil {
		return err
	}

	// Index by subnetId
	subnetKey := []byte(fmt.Sprintf("vs:%s:%020d:%s", rec.SubnetID, rec.StartTimestamp, rec.TxHash))
	if err := batch.Set(subnetKey, []byte{}, pebble.Sync); err != nil {
		return err
	}

	return nil
}

func (v *Validators) getValidator(txHash string) (*ValidatorRecord, error) {
	key := []byte(fmt.Sprintf("v:%s", txHash))
	data, closer, err := v.db.Get(key)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var rec ValidatorRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (v *Validators) getValidatorFromBatch(batch *pebble.Batch, txHash string) (*ValidatorRecord, error) {
	key := []byte(fmt.Sprintf("v:%s", txHash))

	// Try batch first
	data, _, err := batch.Get(key)
	if err == nil {
		var rec ValidatorRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, err
		}
		return &rec, nil
	}

	// Fallback to DB
	return v.getValidator(txHash)
}

func (v *Validators) saveDelegator(batch *pebble.Batch, rec *DelegatorRecord, validatorTxHash string) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	// Primary key
	key := []byte(fmt.Sprintf("d:%s", rec.TxHash))
	if err := batch.Set(key, data, pebble.Sync); err != nil {
		return err
	}

	// Index by validator (for aggregation)
	delKey := []byte(fmt.Sprintf("dn:%s:%s", validatorTxHash, rec.TxHash))
	if err := batch.Set(delKey, []byte{}, pebble.Sync); err != nil {
		return err
	}

	return nil
}

func (v *Validators) getDelegator(txHash string) (*DelegatorRecord, error) {
	key := []byte(fmt.Sprintf("d:%s", txHash))
	data, closer, err := v.db.Get(key)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	var rec DelegatorRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// listValidators returns validators sorted by start time (descending)
func (v *Validators) listValidators(opts ListOptions) ([]*ValidatorRecord, error) {
	var results []*ValidatorRecord

	// Determine prefix based on filters
	var prefix, upper []byte
	if opts.SubnetID != "" {
		prefix = []byte(fmt.Sprintf("vs:%s:", opts.SubnetID))
		upper = []byte(fmt.Sprintf("vs:%s;", opts.SubnetID))
	} else if opts.NodeID != "" {
		prefix = []byte(fmt.Sprintf("vn:%s:", opts.NodeID))
		upper = []byte(fmt.Sprintf("vn:%s;", opts.NodeID))
	} else {
		// Default: iterate all validators by subnet (primary network first)
		prefix = []byte("vs:")
		upper = []byte("vs;")
	}

	iter, _ := v.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	defer iter.Close()

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// Iterate in reverse for descending order
	count := 0
	for iter.Last(); iter.Valid() && count < pageSize; iter.Prev() {
		// Extract txHash from key
		key := string(iter.Key())
		var txHash string
		// Parse key format: vs:{subnetId}:{startTs}:{txHash} or vn:{nodeId}:{startTs}:{txHash}
		parts := splitKey(key, 4)
		if len(parts) >= 4 {
			txHash = parts[3]
		} else {
			continue
		}

		rec, err := v.getValidator(txHash)
		if err != nil {
			continue
		}

		// Apply status filter
		if opts.Status != "" {
			status := rec.ComputeStatus()
			if string(status) != opts.Status {
				continue
			}
		}

		results = append(results, rec)
		count++
	}

	return results, nil
}

// listValidatorsByNodeID returns all validations for a specific nodeId
func (v *Validators) listValidatorsByNodeID(nodeID string, opts ListOptions) ([]*ValidatorRecord, error) {
	opts.NodeID = nodeID
	return v.listValidators(opts)
}

// ListOptions for filtering validator queries
type ListOptions struct {
	PageSize int
	NodeID   string
	SubnetID string
	Status   string // active, pending, completed, removed
}

// splitKey splits a key by : separator
func splitKey(key string, expected int) []string {
	result := make([]string, 0, expected)
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			result = append(result, key[start:i])
			start = i + 1
		}
	}
	if start < len(key) {
		result = append(result, key[start:])
	}
	return result
}

// findValidatorByNodeAndTime finds a validator record by nodeId that was active at a given time
// This is used to match delegators to their validators
func (v *Validators) findValidatorByNodeAndTime(nodeID string, timestamp int64, subnetID string) (*ValidatorRecord, error) {
	prefix := []byte(fmt.Sprintf("vn:%s:", nodeID))
	upper := []byte(fmt.Sprintf("vn:%s;", nodeID))

	iter, _ := v.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		parts := splitKey(string(iter.Key()), 4)
		if len(parts) < 4 {
			continue
		}
		txHash := parts[3]

		rec, err := v.getValidator(txHash)
		if err != nil {
			continue
		}

		// Match subnet and time range
		if rec.SubnetID != subnetID {
			continue
		}
		if timestamp >= rec.StartTimestamp && timestamp <= rec.EndTimestamp {
			return rec, nil
		}
	}

	return nil, pebble.ErrNotFound
}

// Mapping from staking tx to validator tx (for delegators)
func (v *Validators) saveStakingToValidatorMapping(batch *pebble.Batch, stakingTxHash, validatorTxHash string) error {
	key := []byte(fmt.Sprintf("sv:%s", stakingTxHash))
	return batch.Set(key, []byte(validatorTxHash), pebble.Sync)
}

func (v *Validators) getValidatorTxForStaking(stakingTxHash string) (string, error) {
	key := []byte(fmt.Sprintf("sv:%s", stakingTxHash))
	data, closer, err := v.db.Get(key)
	if err != nil {
		return "", err
	}
	defer closer.Close()
	return string(data), nil
}

// updateValidator updates an existing validator record
func (v *Validators) updateValidator(batch *pebble.Batch, rec *ValidatorRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	// Only update main record, indexes already exist
	key := []byte(fmt.Sprintf("v:%s", rec.TxHash))
	return batch.Set(key, data, pebble.Sync)
}

// updateDelegator updates an existing delegator record
func (v *Validators) updateDelegator(batch *pebble.Batch, rec *DelegatorRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	key := []byte(fmt.Sprintf("d:%s", rec.TxHash))
	return batch.Set(key, data, pebble.Sync)
}

// getDelegatorFromBatch tries to get delegator from batch first, then DB
func (v *Validators) getDelegatorFromBatch(batch *pebble.Batch, txHash string) (*DelegatorRecord, error) {
	key := []byte(fmt.Sprintf("d:%s", txHash))

	data, _, err := batch.Get(key)
	if err == nil {
		var rec DelegatorRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, err
		}
		return &rec, nil
	}

	return v.getDelegator(txHash)
}

// countActiveDelegators counts delegators for a validator
func (v *Validators) countDelegators(validatorTxHash string) (int, uint64) {
	prefix := []byte(fmt.Sprintf("dn:%s:", validatorTxHash))
	upper := []byte(fmt.Sprintf("dn:%s;", validatorTxHash))

	iter, _ := v.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	defer iter.Close()

	count := 0
	var totalAmount uint64
	now := time.Now().Unix()

	for iter.First(); iter.Valid(); iter.Next() {
		parts := splitKey(string(iter.Key()), 3)
		if len(parts) < 3 {
			continue
		}
		delTxHash := parts[2]

		del, err := v.getDelegator(delTxHash)
		if err != nil {
			continue
		}

		// Only count active delegators
		if !del.Completed && now >= del.StartTimestamp && now < del.EndTimestamp {
			count++
			// Parse amount
			var amt uint64
			fmt.Sscanf(del.AmountDelegated, "%d", &amt)
			totalAmount += amt
		}
	}

	return count, totalAmount
}

// Used for in-memory caching during batch processing
type pendingValidators struct {
	validators map[string]*ValidatorRecord
	delegators map[string]*DelegatorRecord
}

func (p *pendingValidators) getValidator(txHash string) (*ValidatorRecord, bool) {
	rec, ok := p.validators[txHash]
	return rec, ok
}

func (p *pendingValidators) setValidator(rec *ValidatorRecord) {
	p.validators[rec.TxHash] = rec
}

func (p *pendingValidators) getDelegator(txHash string) (*DelegatorRecord, bool) {
	rec, ok := p.delegators[txHash]
	return rec, ok
}

func (p *pendingValidators) setDelegator(rec *DelegatorRecord) {
	p.delegators[rec.TxHash] = rec
}

var _ io.Closer = (*Validators)(nil)
