package contracts

// JSON strings in the interoperability profile must contain Unicode scalars.
// encoding/json otherwise replaces an unpaired escaped surrogate, while JS
// preserves it. Reject that input rather than silently changing evidence.
func validJSONStrings(data []byte) bool {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		value := hex4(data[i+1 : i+5])
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low := hex4(data[i+3 : i+7])
			if low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func hex4(data []byte) int {
	value := 0
	for _, c := range data {
		value *= 16
		switch {
		case c >= '0' && c <= '9':
			value += int(c - '0')
		case c >= 'a' && c <= 'f':
			value += int(c-'a') + 10
		default:
			value += int(c-'A') + 10
		}
	}
	return value
}
