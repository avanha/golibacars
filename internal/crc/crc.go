// Package crc has the checksums MIAM uses.
package crc

var (
	t16 [256]uint16
	t32 [256]uint32
)

func init() {
	for i := range t16 {
		c := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
		t16[i] = c
	}
	for i := range t32 {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = c<<1 ^ 0x04C11DB7
			} else {
				c <<= 1
			}
		}
		t32[i] = c
	}
}

// CRC16ARINC is la_crc16_arinc: CRC-16 with polynomial 0x1021, most significant bit first.
func CRC16ARINC(data []byte, init uint16) uint16 {
	crc := init
	for _, b := range data {
		crc = crc<<8 ^ t16[byte(crc>>8)^b]
	}
	return crc
}

// CRC32ARINC665 is la_crc32_arinc665: CRC-32 with polynomial 0x04C11DB7, most significant bit first, no final inversion.
func CRC32ARINC665(data []byte, init uint32) uint32 {
	crc := init
	for _, b := range data {
		crc = crc<<8 ^ t32[byte(crc>>24)^b]
	}
	return crc
}
