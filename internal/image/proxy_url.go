package image

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// imageProxySecret 用于 HMAC 签名图片 URL。
// 启动阶段应调用 SetProxySecret 注入稳定密钥;未注入时退回进程级随机密钥。
var (
	imageProxySecretMu sync.RWMutex
	imageProxySecret   []byte
)

func init() {
	imageProxySecret = make([]byte, 32)
	if _, err := rand.Read(imageProxySecret); err != nil {
		for i := range imageProxySecret {
			imageProxySecret[i] = byte(i*31 + 7)
		}
	}
}

// SetProxySecret 注入稳定的图片代理签名密钥。
// seed 不直接作为 HMAC key 使用,统一 SHA-256 后得到固定长度密钥。
func SetProxySecret(seed string) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return
	}
	sum := sha256.Sum256([]byte(seed))
	imageProxySecretMu.Lock()
	imageProxySecret = sum[:]
	imageProxySecretMu.Unlock()
}

// BuildProxyURL 生成代理 URL。返回绝对 path(不含 host)。
func BuildProxyURL(taskID string, idx int, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expMs := time.Now().Add(ttl).UnixMilli()
	sig := computeImgSig(taskID, idx, expMs)
	return fmt.Sprintf("/p/img/%s/%d?exp=%d&sig=%s", taskID, idx, expMs, sig)
}

// ComputeImgSig 计算图片 URL 签名（供 gateway 验证使用）。
func ComputeImgSig(taskID string, idx int, expMs int64) string {
	return computeImgSig(taskID, idx, expMs)
}

func computeImgSig(taskID string, idx int, expMs int64) string {
	imageProxySecretMu.RLock()
	secret := imageProxySecret
	imageProxySecretMu.RUnlock()
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s|%d|%d", taskID, idx, expMs)
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

// VerifyImgSig 验证图片 URL 签名。
func VerifyImgSig(taskID string, idx int, expMs int64, sig string) bool {
	if expMs < time.Now().UnixMilli() {
		return false
	}
	want := computeImgSig(taskID, idx, expMs)
	return hmac.Equal([]byte(sig), []byte(want))
}
