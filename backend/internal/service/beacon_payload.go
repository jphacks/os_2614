package service

// 05 §2.3 の BLE advertise payload（Legacy advertising、合計 26 バイト）から token を取り出す。
//
//	Flags AD:                  02 01 06
//	Manufacturer Specific AD:  16 FF | FFFF（company）| 43 48（"CH"）| 01（version）| token 16
const (
	beaconADManufacturer = 0xFF
	beaconCompanyID      = 0xFFFF
	beaconMagic0         = 0x43 // 'C'
	beaconMagic1         = 0x48 // 'H'
	beaconPayloadVersion = 0x01
	beaconTokenLen       = 16
	beaconNeededData     = 2 + 2 + 1 + beaconTokenLen
)

// ExtractBeaconToken は payload を AD structure として読み、company ID と magic・version が一致する
// Manufacturer Specific Data の token（16 バイト）を返す。形が合わなければ ok=false。
func ExtractBeaconToken(payload []byte) ([]byte, bool) {
	for i := 0; i+1 < len(payload); {
		length := int(payload[i])
		if length == 0 {
			break
		}
		if i+1+length > len(payload) {
			return nil, false
		}
		adType := payload[i+1]
		data := payload[i+2 : i+1+length]
		i += 1 + length

		if adType != beaconADManufacturer || len(data) < beaconNeededData {
			continue
		}
		company := uint16(data[0])<<8 | uint16(data[1])
		if company != beaconCompanyID || data[2] != beaconMagic0 || data[3] != beaconMagic1 || data[4] != beaconPayloadVersion {
			continue
		}
		token := make([]byte, beaconTokenLen)
		copy(token, data[5:5+beaconTokenLen])
		return token, true
	}
	return nil, false
}
