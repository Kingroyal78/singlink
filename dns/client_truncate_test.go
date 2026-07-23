package dns

import (
	"net"
	"testing"

	mDNS "github.com/miekg/dns"
)

func TestTruncateDNSMessageAllocatesForTruncatedLength(t *testing.T) {
	request := new(mDNS.Msg)
	request.SetQuestion("example.com.", mDNS.TypeA)
	response := new(mDNS.Msg)
	response.SetReply(request)
	for response.Len() <= 600 {
		response.Answer = append(response.Answer, &mDNS.A{
			Hdr: mDNS.RR_Header{
				Name:   "example.com.",
				Rrtype: mDNS.TypeA,
				Class:  mDNS.ClassINET,
				Ttl:    60,
			},
			A: net.IPv4(192, 0, 2, byte(len(response.Answer))),
		})
	}
	buffer, err := TruncateDNSMessage(request, response, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer buffer.Release()

	maxCapacity := 8*2 + 1 + 512
	if buffer.Len() > 512 {
		t.Fatalf("truncated message length = %d, want <= 512", buffer.Len())
	}
	if buffer.Cap() > maxCapacity {
		t.Fatalf("buffer cap = %d, want <= %d", buffer.Cap(), maxCapacity)
	}
}
