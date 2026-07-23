package ja3

import "testing"

func TestParseExtensionsRejectsTruncatedSupportedVersions(t *testing.T) {
	var hello ClientHello
	err := hello.parseExtensions([]byte{
		0x00, 0x05, // extensions length
		0x00, 0x2b, // supported_versions
		0x00, 0x01, // extension length
		0x02, // versions length without version bytes
	})
	if err == nil {
		t.Fatal("parseExtensions unexpectedly succeeded")
	}
}

func TestParseExtensionsRejectsTruncatedSignatureAlgorithms(t *testing.T) {
	var hello ClientHello
	err := hello.parseExtensions([]byte{
		0x00, 0x06, // extensions length
		0x00, 0x0d, // signature_algorithms
		0x00, 0x02, // extension length
		0x00, 0x02, // algorithms length without algorithm bytes
	})
	if err == nil {
		t.Fatal("parseExtensions unexpectedly succeeded")
	}
}
