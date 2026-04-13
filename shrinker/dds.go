package main

import (
	"bytes"
	"encoding/binary"
)

const (
	ddsMagicLen   = 4
	ddsHdrLen     = 124
	totalHdrLen   = ddsMagicLen + ddsHdrLen // 128
	pfOffsetInHdr = 76                      // pixel format start inside the 124-byte header
)

// CompressionScheme for a DDS image.
type CompressionScheme int

// Compression schemes.
const (
	Unknown CompressionScheme = iota
	DXT1
	DXT3
	DXT5
	DX10
)

var stringToSchemeMap = map[string]CompressionScheme{
	"DXT1": DXT1,
	"DXT3": DXT3,
	"DXT5": DXT5,
	"DX10": DX10,
}

// DecodeSchema parses the DDS schema for a file.
func DecodeSchema(dds []byte) CompressionScheme {
	if len(dds) < totalHdrLen {
		return Unknown
	}
	if string(dds[0:4]) != "DDS " {
		return Unknown
	}

	// hdr is the 124-byte header (bytes 4..127)
	hdr := dds[4 : 4+ddsHdrLen]

	// Ensure pixel-format block exists
	if len(hdr) < pfOffsetInHdr+32 {
		return Unknown
	}
	pf := hdr[pfOffsetInHdr : pfOffsetInHdr+32]

	// Read canonical pf fields by offset
	pfSize := binary.LittleEndian.Uint32(pf[0:4])
	_ = pfSize // read it for potential validation; not strictly required

	// Some broken exporters set fields oddly. We'll be defensive about locating FourCC.
	// Try pf[4:8] first (many broken files put ASCII FourCC there), then pf[8:12] (canonical),
	// then scan the header as a final fallback.
	var fourCC string
	// helper to test ASCII-like FourCC (DXT1/3/5)
	isDXT := func(b []byte) bool {
		if len(b) < 3 {
			return false
		}
		return (b[0] == 'D' && b[1] == 'X' && b[2] == 'T')
	}

	if isDXT(pf[4:8]) {
		fourCC = string(pf[4:8])
	} else if isDXT(pf[8:12]) {
		fourCC = string(pf[8:12])
	} else {
		// final fallback: scan the 124-byte header for known FourCCs
		for _, s := range []string{"DXT1", "DXT3", "DXT5", "DX10"} {
			if bytes.Contains(hdr, []byte(s)) {
				fourCC = s
				break
			}
		}
	}
	if scheme, ok := stringToSchemeMap[fourCC]; ok {
		return scheme
	} else {
		return Unknown
	}
}
