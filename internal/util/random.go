package util

import (
	crand "crypto/rand"
	"math/big"
)

type generateOpt struct {
	letters string
}
type GenerateOpt = func(*generateOpt)

func WithLetters(letters string) GenerateOpt {
	return func(o *generateOpt) {
		o.letters = letters
	}
}

func WithAlphaNumer() GenerateOpt {
	return func(o *generateOpt) {
		o.letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	}
}

// GenerateRandomString 生成指定长度的随机字符串
func GenerateRandomString(n int, opts ...GenerateOpt) string {
	opt := &generateOpt{
		letters: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=~,./;",
	}
	for _, o := range opts {
		o(opt)
	}
	var letters = opt.letters
	var size = int64(len(letters))
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := crand.Int(crand.Reader, big.NewInt(size))
		if err != nil {
			result[i] = letters[i%len(letters)]
			continue
		}
		result[i] = letters[v.Int64()]
	}
	return string(result)
}
