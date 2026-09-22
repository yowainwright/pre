package manager

import (
	"bytes"
	"encoding/json"
	"errors"
)

func unmarshalBunLock(data []byte, target any) error {
	if index := bytes.IndexByte(data, '{'); index >= 0 {
		data = data[index:]
	}
	withoutComments, err := stripJSONCComments(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(stripJSONCTrailingCommas(withoutComments), target)
}

func stripJSONCComments(data []byte) ([]byte, error) {
	result := make([]byte, 0, len(data))
	for index := 0; index < len(data); {
		var err error
		result, index, err = copyJSONCPart(result, data, index)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func copyJSONCPart(result, data []byte, index int) ([]byte, int, error) {
	if data[index] == '"' {
		next := copyJSONString(data, index)
		return append(result, data[index:next]...), next, nil
	}
	if startsJSONCComment(data, index, '/') {
		return result, skipJSONCLineComment(data, index+2), nil
	}
	if startsJSONCComment(data, index, '*') {
		return skipJSONCBlockComment(result, data, index+2)
	}
	return append(result, data[index]), index + 1, nil
}

func startsJSONCComment(data []byte, index int, marker byte) bool {
	if index+1 >= len(data) {
		return false
	}
	startsComment := data[index] == '/' && data[index+1] == marker
	return startsComment
}

func copyJSONString(data []byte, start int) int {
	escaped := false
	for index := start + 1; index < len(data); index++ {
		endsString := data[index] == '"' && !escaped
		if endsString {
			return index + 1
		}
		startsEscape := data[index] == '\\' && !escaped
		if startsEscape {
			escaped = true
			continue
		}
		escaped = false
	}
	return len(data)
}

func skipJSONCLineComment(data []byte, index int) int {
	for index < len(data) && data[index] != '\n' {
		index++
	}
	return index
}

func skipJSONCBlockComment(result, data []byte, index int) ([]byte, int, error) {
	for index+1 < len(data) {
		if data[index] == '\n' {
			result = append(result, '\n')
		}
		endsComment := data[index] == '*' && data[index+1] == '/'
		if endsComment {
			return result, index + 2, nil
		}
		index++
	}
	return nil, 0, errors.New("unterminated JSONC block comment")
}

func stripJSONCTrailingCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for index := 0; index < len(data); index++ {
		if data[index] == '"' {
			next := copyJSONString(data, index)
			result = append(result, data[index:next]...)
			index = next - 1
			continue
		}
		trailingComma := data[index] == ',' && jsonCCommaIsTrailing(data, index)
		if trailingComma {
			continue
		}
		result = append(result, data[index])
	}
	return result
}

func jsonCCommaIsTrailing(data []byte, index int) bool {
	for index++; index < len(data); index++ {
		switch data[index] {
		case ' ', '\t', '\r', '\n':
			continue
		case '}', ']':
			return true
		default:
			return false
		}
	}
	return false
}
