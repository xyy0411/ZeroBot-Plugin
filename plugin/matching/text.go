package matching

import "fmt"

const (
	defaultProfileExpire       = 1800
	defaultProfileExpireBlock  = 2000
	defaultProfileExpirePrompt = 2400
)

func parseSoftwareType(input string) (int8, bool) {
	switch input {
	case "主副皆可", "主副":
		return 0, true
	case "仅主", "主":
		return 1, true
	case "仅副", "副":
		return 2, true
	default:
		return 0, false
	}
}

func softwareTypeName(softwareType int8) string {
	switch softwareType {
	case 0:
		return "主副皆可"
	case 1:
		return "仅主"
	case 2:
		return "仅副"
	default:
		return fmt.Sprintf("未知类型(%d)", softwareType)
	}
}
