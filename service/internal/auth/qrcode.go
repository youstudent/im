// qrcode.go 实现二维码登录：一次性 qrcodeId 生成 / 轮询 / 确认。
// 流程：桌面端 create 生成带过期的 qrcodeId → 手机扫码后 confirm（回写状态与 uid）
// → 桌面端 poll 到 confirmed 后签发 token（并标记已消费，防止重复签发）。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/log"
	"im/service/internal/store/mysql"
)

// 二维码状态枚举。
const (
	QRStatusPending   = "pending"   // 等待扫码
	QRStatusScanned   = "scanned"   // 已扫码待确认
	QRStatusConfirmed = "confirmed" // 已确认，可签发
	QRStatusExpired   = "expired"   // 已过期/已消费
)

// 二维码有效期（默认 5 分钟，与前端提示一致）。
const qrTTL = 5 * time.Minute

// CreateQRResult 创建二维码返回给桌面端。
type CreateQRResult struct {
	QRCodeID string `json:"qrcode_id"`  // 一次性二维码 ID
	ExpireIn int64  `json:"expire_in"`  // 有效期（秒）
	Payload  string `json:"payload"`    // 二维码内容（编码 qrcode_id，供扫码识别）
}

// QRState 二维码状态结构（存 Redis）。
type QRState struct {
	Status   string `json:"status"`
	UID      int64  `json:"uid,omitempty"`      // 确认者 uid（confirmed 后填充）
	Token    string `json:"token,omitempty"`    // 确认时签发的临时 token（scanned 后可校验）
}

// CreateQR 生成一次性二维码。
func (s *Service) CreateQR() (*CreateQRResult, error) {
	id, err := randomID()
	if err != nil {
		return nil, apperr.WrapInternal("生成二维码 ID 失败", err)
	}
	state := &QRState{Status: QRStatusPending}
	if err := s.setQRState(id, state); err != nil {
		return nil, err
	}
	return &CreateQRResult{
		QRCodeID: id,
		ExpireIn: int64(qrTTL.Seconds()),
		Payload:  "workchat:qrcode:" + id,
	}, nil
}

// ConfirmQRReq 手机端确认请求。
// 注意：确认者 uid 由 JWT 鉴权后从令牌上下文注入（见 handler），
// 不再从请求体读取——旧版任意传 uid 即可冒充确认，可直接接管任意账号（审计 P0）。
type ConfirmQRReq struct {
	QRCodeID string `json:"qrcode_id"`
}

// ConfirmQR 手机扫码确认：回写状态为 confirmed 并记录确认者 uid。
// uid 必须是已登录的确认者本人（handler 从 JWT 注入），不信任客户端传入。
func (s *Service) ConfirmQR(uid int64, req *ConfirmQRReq) error {
	id := trimSpace(req.QRCodeID)
	if id == "" || uid <= 0 {
		return apperr.BadRequest("参数不完整")
	}
	state, err := s.getQRState(id)
	if err != nil {
		if errors.Is(err, mysql.ErrNotFound) {
			return apperr.NotFound("二维码已失效")
		}
		return err
	}
	if state.Status != QRStatusPending {
		return apperr.Conflict("二维码状态已变更")
	}
	state.Status = QRStatusConfirmed
	state.UID = uid
	if err := s.setQRState(id, state); err != nil {
		return err
	}
	log.L().Info("qrcode confirmed", "qrcode_id", id, "uid", uid)
	return nil
}

// PollQRReq 轮询请求。
type PollQRReq struct {
	QRCodeID string `json:"qrcode_id"`
}

// PollQRResult 轮询结果。
type PollQRResult struct {
	Status string `json:"status"` // pending/scanned/confirmed/expired
	// confirmed 且首次签发时返回令牌（签发后状态置 expired，下次返回 expired）
	Login *LoginResult `json:"login,omitempty"`
}

// PollQR 桌面端轮询二维码状态；confirmed 时签发 token 并消费二维码。
func (s *Service) PollQR(req *PollQRReq) (*PollQRResult, error) {
	id := trimSpace(req.QRCodeID)
	if id == "" {
		return nil, apperr.BadRequest("缺少二维码 ID")
	}
	state, err := s.getQRState(id)
	if err != nil {
		if errors.Is(err, mysql.ErrNotFound) {
			return &PollQRResult{Status: QRStatusExpired}, nil
		}
		return nil, err
	}
	switch state.Status {
	case QRStatusPending, QRStatusScanned:
		return &PollQRResult{Status: state.Status}, nil
	case QRStatusConfirmed:
		// 签发 token 并消费
		res, err := s.issueTokens(state.UID)
		if err != nil {
			return nil, err
		}
		state.Status = QRStatusExpired
		_ = s.setQRState(id, state) // 消费失败仅影响重复签发兜底
		return &PollQRResult{Status: QRStatusConfirmed, Login: res}, nil
	default:
		return &PollQRResult{Status: QRStatusExpired}, nil
	}
}

func (s *Service) setQRState(id string, state *QRState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return apperr.WrapInternal("序列化二维码状态失败", err)
	}
	if err := s.cache.Set(qrKey(id), string(data), qrTTL); err != nil {
		return apperr.WrapInternal("保存二维码状态失败", err)
	}
	return nil
}

func (s *Service) getQRState(id string) (*QRState, error) {
	raw, err := s.cache.Get(qrKey(id))
	if err != nil || raw == "" {
		return nil, mysql.ErrNotFound
	}
	var state QRState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, apperr.WrapInternal("解析二维码状态失败", err)
	}
	return &state, nil
}

func qrKey(id string) string { return "auth:qrcode:" + id }

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
