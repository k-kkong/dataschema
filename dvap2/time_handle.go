package dvap2

import (
	"strconv"
	"strings"
	"time"
)

// 接受 yyyy-dd-mm hh:mm:ss 格式
func StringToDate(date_str string) time.Time {

	chunks := strings.Split(date_str, " ")
	var year, month, day, hour, min, sec, nsec int

	if len(chunks) > 0 {
		schunks := strings.Split(chunks[0], "-")
		if len(schunks) > 0 {
			// year ,_= strconv.Atoi(schunks[0])
			year, _ = strconv.Atoi(schunks[0])

		}
		if len(schunks) > 1 {
			month, _ = strconv.Atoi(schunks[1])
		}

		if len(schunks) > 2 {
			// day ,_= strconv.Atoi(schunks[2])
			day, _ = strconv.Atoi(schunks[2])
		}
	}

	if len(chunks) > 1 {
		schunks := strings.Split(chunks[1], ":")
		if len(schunks) > 0 {
			hour, _ = strconv.Atoi(schunks[0])
		}

		if len(schunks) > 1 {
			min, _ = strconv.Atoi(schunks[1])
		}

		if len(schunks) > 2 {
			// if strings.Contains(schunks[2], ".") {
			sschunks := strings.Split(schunks[2], ".")
			if len(sschunks) > 0 {
				sec, _ = strconv.Atoi(sschunks[0])
			}
			if len(sschunks) > 1 {
				nsec, _ = strconv.Atoi(sschunks[1])
			}
			// }
		}
	}
	return time.Date(year, time.Month(month), day, hour, min, sec, nsec, time.Local)

}

func Validate(t time.Time) bool {
	return t.After(time.Date(1971, 1, 1, 0, 0, 0, 0, time.Local))
}

func ValidateString(value string) bool {
	t := StringToDate(value)
	return t.After(time.Date(1971, 1, 1, 0, 0, 0, 0, time.Local))
}
