package translatorbot

import (
	"strconv"
	"time"
)

const discordEpochMillis = 1420070400000

func discordSnowflakeTime(id string) (time.Time, bool) {
	if len(id) < 17 {
		return time.Time{}, false
	}
	snowflake, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	timestampMillis := int64(snowflake>>22) + discordEpochMillis
	return time.UnixMilli(timestampMillis).UTC(), true
}

func snowflakeIDBefore(cutoff time.Time) int64 {
	return (cutoff.UnixMilli() - discordEpochMillis) << 22
}
