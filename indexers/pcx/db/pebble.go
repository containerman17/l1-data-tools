package db

import (
	"log"

	"github.com/cockroachdb/pebble/v2"
)

// quietLogger silences info logs, keeps errors
type quietLogger struct{}

func (quietLogger) Infof(format string, args ...interface{}) {}
func (quietLogger) Errorf(format string, args ...interface{}) {
	log.Printf("[pebble] "+format, args...)
}
func (quietLogger) Fatalf(format string, args ...interface{}) {
	log.Fatalf("[pebble] "+format, args...)
}

// QuietLogger returns a pebble logger that only logs errors
func QuietLogger() pebble.Logger {
	return quietLogger{}
}

func GetWatermark(db *pebble.DB, key string) (uint64, error) {
	val, closer, err := db.Get([]byte(key))
	if err == pebble.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	if len(val) != 8 {
		return 0, nil
	}
	return uint64(val[0])<<56 | uint64(val[1])<<48 | uint64(val[2])<<40 | uint64(val[3])<<32 |
		uint64(val[4])<<24 | uint64(val[5])<<16 | uint64(val[6])<<8 | uint64(val[7]), nil
}

func SaveWatermark(batch *pebble.Batch, key string, watermark uint64) {
	b := make([]byte, 8)
	b[0] = byte(watermark >> 56)
	b[1] = byte(watermark >> 48)
	b[2] = byte(watermark >> 40)
	b[3] = byte(watermark >> 32)
	b[4] = byte(watermark >> 24)
	b[5] = byte(watermark >> 16)
	b[6] = byte(watermark >> 8)
	b[7] = byte(watermark)
	batch.Set([]byte(key), b, nil)
}
