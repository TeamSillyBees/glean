package auth

import "golang.org/x/crypto/bcrypt"

// PasswordManager 定义密码哈希与校验能力，便于在 service 层保持显式依赖。
type PasswordManager interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

// BcryptPasswordManager 提供基于 bcrypt 的默认密码实现。
type BcryptPasswordManager struct {
	cost int
}

// NewBcryptPasswordManager 创建一个基于 bcrypt 的密码管理器。
func NewBcryptPasswordManager(cost int) *BcryptPasswordManager {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	return &BcryptPasswordManager{cost: cost}
}

// Hash 对明文密码做不可逆哈希。
func (m *BcryptPasswordManager) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), m.cost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// Compare 校验明文密码与哈希值是否匹配。
func (m *BcryptPasswordManager) Compare(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
