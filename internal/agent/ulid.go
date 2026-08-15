package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewULID 生成一个"类 ULID"的唯一 ID：时间前缀（毫秒）+ 随机后缀。
// 不引入第三方依赖，满足排序性与唯一性需求。
func NewULID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败时退回时间戳 + 计数器保证不阻塞。
		return fmt.Sprintf("%x%x", time.Now().UnixNano(), b)
	}
	return fmt.Sprintf("%x%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}
