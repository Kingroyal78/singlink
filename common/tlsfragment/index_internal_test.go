package tf

import "testing"

func TestIndexTLSServerNameFromExtensionsRejectsTruncatedName(t *testing.T) {
	serverName := indexTLSServerNameFromExtensions([]byte{
		0x00, 0x09, // extensions length
		0x00, 0x00, // SNI extension
		0x00, 0x05, // extension length
		0x00, 0x03, // server name list length
		0x00,       // host name type
		0x00, 0x02, // host name length without host name bytes
	})
	if serverName != nil {
		t.Fatalf("server name = %+v, want nil", serverName)
	}
}
