package network_stats

type NetworkStats struct {
	DelegatorDetails DelegatorDetails `json:"delegatorDetails"`
	ValidatorDetails ValidatorDetails `json:"validatorDetails"`
}

type DelegatorDetails struct {
	DelegatorCount    int    `json:"delegatorCount"`
	TotalAmountStaked string `json:"totalAmountStaked"`
}

type ValidatorDetails struct {
	ValidatorCount               int                `json:"validatorCount"`
	TotalAmountStaked            string             `json:"totalAmountStaked"`
	EstimatedAnnualStakingReward string             `json:"estimatedAnnualStakingReward"`
	StakingDistributionByVersion []VersionStatistic `json:"stakingDistributionByVersion"`
	StakingRatio                 string             `json:"stakingRatio"`
}

type VersionStatistic struct {
	Version        string `json:"version"`
	AmountStaked   string `json:"amountStaked"`
	ValidatorCount int    `json:"validatorCount"`
}
