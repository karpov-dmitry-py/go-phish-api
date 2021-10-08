package validate

func TrimBytes(body []byte) string {
	var result string
	if len(body) < 500 {
		result = string(body)
	} else {
		result = string(body[:200]) + "...(CUT)..." + string(body[len(body)-200:])
	}
	return result
}
