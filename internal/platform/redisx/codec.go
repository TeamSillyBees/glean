package redisx

import (
	"encoding/json"

	"github.com/vmihailenco/msgpack/v5"
)

// Codec 描述 Redis 缓存值序列化协议。
type Codec interface {
	Name() string
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, value any) error
}

// JSONCodec 使用 encoding/json 编解码。
type JSONCodec struct{}

// Name 返回编解码器名称。
func (JSONCodec) Name() string {
	return "json"
}

// Marshal 将值编码为 JSON。
func (JSONCodec) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

// Unmarshal 将 JSON 解码到目标值。
func (JSONCodec) Unmarshal(data []byte, value any) error {
	return json.Unmarshal(data, value)
}

// MsgpackCodec 使用 msgpack 编解码。
type MsgpackCodec struct{}

// Name 返回编解码器名称。
func (MsgpackCodec) Name() string {
	return "msgpack"
}

// Marshal 将值编码为 Msgpack。
func (MsgpackCodec) Marshal(value any) ([]byte, error) {
	return msgpack.Marshal(value)
}

// Unmarshal 将 Msgpack 解码到目标值。
func (MsgpackCodec) Unmarshal(data []byte, value any) error {
	return msgpack.Unmarshal(data, value)
}
