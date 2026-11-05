package chatgpt

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha3"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
)

type fingerprint struct {
	PageOrigin string  `json:"pageOrigin"`
	UserAgent  string  `json:"userAgent"`
	DeviceID   string  `json:"deviceId"`
	Language   string  `json:"language"`
	Languages  string  `json:"languages"`
	Script     string  `json:"script"`
	Build      string  `json:"build"`
	Date       string  `json:"date"`
	Timezone   string  `json:"timezone"`
	Offset     int     `json:"offset"`
	Screen     int     `json:"screen"`
	Cores      int     `json:"cores"`
	Now        float64 `json:"now"`
	Origin     float64 `json:"origin"`
}

func uuid() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6], b[8] = b[6]&15|64, b[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (f fingerprint) proofConfig() []any {
	return []any{f.Screen, f.Date, 4294705152, 1, f.UserAgent, f.Script, f.Build,
		f.Language, f.Languages, rand.Float64(), "webdriver−false", "location", "window",
		f.Now, uuid(), "", max(f.Cores, 1), f.Origin, 0, 0, 0, 0, 0, 0, 1}
}

func requirementsToken(config []any) string {
	b, _ := json.Marshal(config)
	return "gAAAAAC" + base64.StdEncoding.EncodeToString(b)
}

// proofToken follows chatgpt2api's SHA3-512 challenge with the account's real Firefox identity.
func proofToken(ctx context.Context, seed, difficulty string, config []any) (string, error) {
	target, err := hex.DecodeString(difficulty)
	if err != nil || len(target) == 0 || len(target) > 64 || len(config) != 25 {
		return "", errors.New("ChatGPT 工作量证明参数无效")
	}
	config = append([]any(nil), config...)
	for i := range 500000 {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		config[3], config[9] = i, i>>1
		b, err := json.Marshal(config)
		if err != nil {
			return "", err
		}
		encoded := base64.StdEncoding.EncodeToString(b)
		digest := sha3.Sum512([]byte(seed + encoded))
		if bytes.Compare(digest[:len(target)], target) <= 0 {
			return "gAAAAAB" + encoded, nil
		}
	}
	return "", errors.New("ChatGPT 工作量证明未完成，请稍后重试")
}
