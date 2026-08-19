// Package pwd 提供密码哈希与校验（bcrypt）。
package pwd

import "golang.org/x/crypto/bcrypt"

// Hash 使用 bcrypt 对明文密码加盐哈希。
func Hash(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Verify 校验明文密码与哈希是否匹配。
func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
