package factory

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"math/big"
)

// sha512Crypt generates a SHA-512 crypt hash ($6$) for the given password.
func sha512Crypt(password string) (string, error) {
	salt, err := generateCryptSalt(16)
	if err != nil {
		return "", err
	}
	return sha512CryptWithSalt(password, salt), nil
}

func generateCryptSalt(length int) (string, error) {
	const saltChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789./"
	salt := make([]byte, length)
	for i := range salt {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(saltChars))))
		if err != nil {
			return "", fmt.Errorf("generate random salt: %w", err)
		}
		salt[i] = saltChars[n.Int64()]
	}
	return string(salt), nil
}

func sha512CryptWithSalt(password, salt string) string {
	pass := []byte(password)
	saltB := []byte(salt)
	pLen := len(pass)

	// Step 1-3: Compute digest B
	bCtx := sha512.New()
	bCtx.Write(pass)
	bCtx.Write(saltB)
	bCtx.Write(pass)
	bHash := bCtx.Sum(nil)

	// Step 4-8: Compute digest A
	aCtx := sha512.New()
	aCtx.Write(pass)
	aCtx.Write(saltB)
	for i := pLen; i > 64; i -= 64 {
		aCtx.Write(bHash)
	}
	remainder := pLen % 64
	if remainder == 0 && pLen > 0 {
		remainder = 64
	}
	aCtx.Write(bHash[:remainder])

	// Step 9-10: Bit mixing based on password length
	for i := pLen; i > 0; i >>= 1 {
		if i%2 != 0 {
			aCtx.Write(bHash)
		} else {
			aCtx.Write(pass)
		}
	}
	aHash := aCtx.Sum(nil)

	// Step 11: Compute DP
	dpCtx := sha512.New()
	for i := 0; i < pLen; i++ {
		dpCtx.Write(pass)
	}
	dpHash := dpCtx.Sum(nil)

	// Step 12: Produce P string
	pStr := make([]byte, pLen)
	idx := 0
	for idx+64 <= pLen {
		copy(pStr[idx:], dpHash)
		idx += 64
	}
	if idx < pLen {
		copy(pStr[idx:], dpHash[:pLen-idx])
	}

	// Step 13: Compute DS
	dsCtx := sha512.New()
	for i := 0; i < 16+int(aHash[0]); i++ {
		dsCtx.Write(saltB)
	}
	dsHash := dsCtx.Sum(nil)

	// Step 14: Produce S string
	sLen := len(saltB)
	sStr := make([]byte, sLen)
	idx = 0
	for idx+64 <= sLen {
		copy(sStr[idx:], dsHash)
		idx += 64
	}
	if idx < sLen {
		copy(sStr[idx:], dsHash[:sLen-idx])
	}

	// Step 15-20: 5000 rounds
	cHash := aHash
	for round := 0; round < 5000; round++ {
		ctx := sha512.New()
		if round%2 != 0 {
			ctx.Write(pStr)
		} else {
			ctx.Write(cHash)
		}
		if round%3 != 0 {
			ctx.Write(sStr)
		}
		if round%7 != 0 {
			ctx.Write(pStr)
		}
		if round%2 != 0 {
			ctx.Write(cHash)
		} else {
			ctx.Write(pStr)
		}
		cHash = ctx.Sum(nil)
	}

	// Step 21: Encode
	encoded := sha512CryptEncode64(cHash)
	return fmt.Sprintf("$6$%s$%s", salt, encoded)
}

func sha512CryptEncode64(hash []byte) string {
	const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, 0, 86)

	encode := func(a, b, c byte, n int) {
		v := uint(a)<<16 | uint(b)<<8 | uint(c)
		for i := 0; i < n; i++ {
			buf = append(buf, itoa64[v&0x3f])
			v >>= 6
		}
	}

	encode(hash[0], hash[21], hash[42], 4)
	encode(hash[22], hash[43], hash[1], 4)
	encode(hash[44], hash[2], hash[23], 4)
	encode(hash[3], hash[24], hash[45], 4)
	encode(hash[25], hash[46], hash[4], 4)
	encode(hash[47], hash[5], hash[26], 4)
	encode(hash[6], hash[27], hash[48], 4)
	encode(hash[28], hash[49], hash[7], 4)
	encode(hash[50], hash[8], hash[29], 4)
	encode(hash[9], hash[30], hash[51], 4)
	encode(hash[31], hash[52], hash[10], 4)
	encode(hash[53], hash[11], hash[32], 4)
	encode(hash[12], hash[33], hash[54], 4)
	encode(hash[34], hash[55], hash[13], 4)
	encode(hash[56], hash[14], hash[35], 4)
	encode(hash[15], hash[36], hash[57], 4)
	encode(hash[37], hash[58], hash[16], 4)
	encode(hash[59], hash[17], hash[38], 4)
	encode(hash[18], hash[39], hash[60], 4)
	encode(hash[40], hash[61], hash[19], 4)
	encode(hash[62], hash[20], hash[41], 4)
	encode(0, 0, hash[63], 2)

	return string(buf)
}
