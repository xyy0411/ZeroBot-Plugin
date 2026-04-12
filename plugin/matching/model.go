package matching

// RejectedMatchUser 用于检查是否已被用户拒绝过匹配邀请
type RejectedMatchUser struct {
	ID     uint  `gorm:"primary_key"`
	UserID int64 `gorm:"unique"`
}

type Matching struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`
	ExpireAt int64  `json:"expire_at"`

	BlockUsers      []BlockUser      `json:"block_users"`
	OnlineSoftwares []OnlineSoftware `json:"online_softwares"`
}

type BlockUser struct {
	UserID int64 `json:"user_id"`
}

type OnlineSoftware struct {
	Name string `json:"name"`
	Type int8   `json:"type"`
}
