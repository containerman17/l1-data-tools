package transform

import (
	"github.com/containerman17/l1-data-tools/ingestion/evm/rpc/rpc"
)

// TransformLog converts an RPC Log to a LogRow.
func TransformLog(b *rpc.Block, log *rpc.Log) LogRow {
	ts := parseHexInt(b.Timestamp)

	// Extract up to 4 topics
	topicHex := [4]string{}
	topicDec := [4]string{}
	for i := 0; i < len(log.Topics) && i < 4; i++ {
		topicHex[i] = log.Topics[i]
		topicDec[i] = formatDecimalOrEmpty(log.Topics[i])
	}

	return LogRow{
		BlockHash:        b.Hash,
		BlockNumber:      parseHexInt(b.Number),
		BlockTimestamp:   ts,
		TransactionHash:  log.TransactionHash,
		LogAddress:       log.Address,
		TopicHex0:        topicHex[0],
		TopicHex1:        topicHex[1],
		TopicHex2:        topicHex[2],
		TopicHex3:        topicHex[3],
		TopicDec0:        topicDec[0],
		TopicDec1:        topicDec[1],
		TopicDec2:        topicDec[2],
		TopicDec3:        topicDec[3],
		LogData:          log.Data,
		LogIndex:         parseHexInt(log.LogIndex),
		TransactionIndex: parseHexInt(log.TransactionIndex),
		Removed:          log.Removed,
		PartitionDate:    partitionDate(ts),
	}
}
