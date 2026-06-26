package v2board

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	maxMsgpackDepth           = 64
	maxMsgpackContainerLength = 1 << 20
)

func userListFromAny(value any) (*UserListBody, error) {
	value = unwrapUserList(value)
	rawUsers, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("v2board: unexpected user list payload %T", value)
	}
	users := make([]UserInfo, 0, len(rawUsers))
	for _, rawUser := range rawUsers {
		userMap, ok := rawUser.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("v2board: unexpected user payload %T", rawUser)
		}
		users = append(users, UserInfo{
			ID:             int(firstInt(userMap, "id", "uid")),
			UUID:           userUUID(userMap),
			SpeedLimit:     int(firstInt(userMap, "speed_limit")),
			DeviceLimit:    int(firstInt(userMap, "device_limit")),
			Label:          stringValue(userMap["label"]),
			Secret:         stringValue(userMap["secret"]),
			LinkSecret:     stringValue(userMap["link_secret"]),
			Port:           int(firstInt(userMap, "port")),
			Cipher:         stringValue(userMap["cipher"]),
			Enabled:        boolValue(userMap["enabled"]),
			EnabledSet:     hasKey(userMap, "enabled"),
			MaxConnections: int(firstInt(userMap, "max_connections")),
			MaxIPs:         int(firstInt(userMap, "max_ips")),
			QuotaBytes:     firstInt(userMap, "quota_bytes"),
			ExpiresAt:      firstInt(userMap, "expires_at"),
			ExpiresOn:      stringValue(userMap["expires_on"]),
		})
	}
	return &UserListBody{Users: users}, nil
}

func unwrapUserList(value any) any {
	values, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if users, ok := values["users"]; ok {
		return users
	}
	if data, ok := values["data"]; ok {
		return data
	}
	return value
}

func userUUID(values map[string]any) string {
	if uuid := stringValue(values["uuid"]); uuid != "" {
		return uuid
	}
	if secret := stringValue(values["secret"]); secret != "" {
		return secret
	}
	if nested := mapValue(values["v2ray_user"]); nested != nil {
		if uuid := stringValue(nested["uuid"]); uuid != "" {
			return uuid
		}
	}
	if nested := mapValue(values["trojan_user"]); nested != nil {
		if password := stringValue(nested["password"]); password != "" {
			return password
		}
	}
	return ""
}

func hasKey(values map[string]any, key string) bool {
	_, exists := values[key]
	return exists
}

func mapValue(value any) map[string]any {
	typedValue, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typedValue
}

func firstInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, exists := values[key]; exists {
			return intValue(value)
		}
	}
	return 0
}

func intValue(value any) int64 {
	switch typedValue := value.(type) {
	case int:
		return int64(typedValue)
	case int8:
		return int64(typedValue)
	case int16:
		return int64(typedValue)
	case int32:
		return int64(typedValue)
	case int64:
		return typedValue
	case uint:
		return int64(typedValue)
	case uint8:
		return int64(typedValue)
	case uint16:
		return int64(typedValue)
	case uint32:
		return int64(typedValue)
	case uint64:
		return int64(typedValue)
	case float64:
		return int64(typedValue)
	case json.Number:
		number, _ := typedValue.Int64()
		return number
	default:
		return 0
	}
}

func stringValue(value any) string {
	if typedValue, ok := value.(string); ok {
		return typedValue
	}
	return ""
}

func boolValue(value any) bool {
	if typedValue, ok := value.(bool); ok {
		return typedValue
	}
	return intValue(value) != 0
}

type msgpackDecoder struct {
	data []byte
	pos  int
}

func decodeMsgpack(data []byte) (any, error) {
	decoder := &msgpackDecoder{data: data}
	value, err := decoder.readValue(0)
	if err != nil {
		return nil, err
	}
	if decoder.pos != len(data) {
		return nil, fmt.Errorf("trailing msgpack data at byte %d", decoder.pos)
	}
	return value, nil
}

func (d *msgpackDecoder) readValue(depth int) (any, error) {
	if depth > maxMsgpackDepth {
		return nil, fmt.Errorf("msgpack nesting too deep")
	}
	code, err := d.readByte()
	if err != nil {
		return nil, err
	}
	switch {
	case code <= 0x7f:
		return int64(code), nil
	case code >= 0xe0:
		return int64(int8(code)), nil
	case code >= 0xa0 && code <= 0xbf:
		return d.readString(int(code & 0x1f))
	case code >= 0x90 && code <= 0x9f:
		return d.readArray(int(code&0x0f), depth+1)
	case code >= 0x80 && code <= 0x8f:
		return d.readMap(int(code&0x0f), depth+1)
	}

	switch code {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xcc:
		value, err := d.readByte()
		return int64(value), err
	case 0xcd:
		value, err := d.readUint16()
		return int64(value), err
	case 0xce:
		value, err := d.readUint32()
		return int64(value), err
	case 0xcf:
		value, err := d.readUint64()
		return int64(value), err
	case 0xd0:
		value, err := d.readByte()
		return int64(int8(value)), err
	case 0xd1:
		value, err := d.readUint16()
		return int64(int16(value)), err
	case 0xd2:
		value, err := d.readUint32()
		return int64(int32(value)), err
	case 0xd3:
		value, err := d.readUint64()
		return int64(value), err
	case 0xd9:
		length, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readString(int(length))
	case 0xda:
		length, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readString(int(length))
	case 0xdb:
		length, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readString(int(length))
	case 0xdc:
		length, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readArray(int(length), depth+1)
	case 0xdd:
		length, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		if length > uint32(maxInt()) {
			return nil, fmt.Errorf("msgpack array too large: %d", length)
		}
		return d.readArray(int(length), depth+1)
	case 0xde:
		length, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readMap(int(length), depth+1)
	case 0xdf:
		length, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		if length > uint32(maxInt()) {
			return nil, fmt.Errorf("msgpack map too large: %d", length)
		}
		return d.readMap(int(length), depth+1)
	default:
		return nil, fmt.Errorf("unsupported msgpack code 0x%x", code)
	}
}

func (d *msgpackDecoder) readArray(length int, depth int) ([]any, error) {
	if length > maxMsgpackContainerLength {
		return nil, fmt.Errorf("msgpack array too large: %d", length)
	}
	if length > d.remaining() {
		return nil, io.ErrUnexpectedEOF
	}
	values := make([]any, length)
	for index := range values {
		value, err := d.readValue(depth)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func (d *msgpackDecoder) readMap(length int, depth int) (map[string]any, error) {
	if length > maxMsgpackContainerLength {
		return nil, fmt.Errorf("msgpack map too large: %d", length)
	}
	if length > d.remaining()/2 {
		return nil, io.ErrUnexpectedEOF
	}
	values := make(map[string]any, length)
	for range length {
		key, err := d.readValue(depth)
		if err != nil {
			return nil, err
		}
		keyString, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("unsupported msgpack map key %T", key)
		}
		value, err := d.readValue(depth)
		if err != nil {
			return nil, err
		}
		values[keyString] = value
	}
	return values, nil
}

func (d *msgpackDecoder) readString(length int) (string, error) {
	value, err := d.readBytes(length)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func (d *msgpackDecoder) readByte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, io.ErrUnexpectedEOF
	}
	value := d.data[d.pos]
	d.pos++
	return value, nil
}

func (d *msgpackDecoder) remaining() int {
	return len(d.data) - d.pos
}

func (d *msgpackDecoder) readBytes(length int) ([]byte, error) {
	if length < 0 || d.pos+length > len(d.data) {
		return nil, io.ErrUnexpectedEOF
	}
	value := d.data[d.pos : d.pos+length]
	d.pos += length
	return value, nil
}

func (d *msgpackDecoder) readUint16() (uint16, error) {
	value, err := d.readBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(value), nil
}

func (d *msgpackDecoder) readUint32() (uint32, error) {
	value, err := d.readBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(value), nil
}

func (d *msgpackDecoder) readUint64() (uint64, error) {
	value, err := d.readBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(value), nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
