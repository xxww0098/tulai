package cdp

import "encoding/json"

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseJSValue 解析 Evaluate 返回的值到 v。
// JS 表达式若返回 JSON.stringify(...) 的字符串，CDP 给出的是"字符串的 JSON"，
// 需要先解一层字符串再解对象；兼容直接返回对象的情况。
func ParseJSValue(out string, v any) error {
	if err := json.Unmarshal([]byte(out), v); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return err
	}
	return json.Unmarshal([]byte(s), v)
}
