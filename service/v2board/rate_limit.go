package v2board

import (
	"context"
	"net"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"
)

type rateLimiter = rate.Limiter

func newRateLimiter(speedLimitMbps int) *rateLimiter {
	bytesPerSecond := speedLimitBytesPerSecond(speedLimitMbps)
	if bytesPerSecond <= 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(bytesPerSecond), bytesPerSecond)
}

func speedLimitBytesPerSecond(speedLimitMbps int) int {
	if speedLimitMbps <= 0 {
		return 0
	}
	bytesPerSecond := int64(speedLimitMbps) * 1000 * 1000 / 8
	maxInt := int64(int(^uint(0) >> 1))
	if bytesPerSecond > maxInt {
		return int(maxInt)
	}
	return int(bytesPerSecond)
}

type rateLimitedConn struct {
	N.ExtendedConn
	ctx    context.Context
	cancel context.CancelFunc
	limit  *rateLimiter
}

func newRateLimitedConn(conn net.Conn, limiter *rateLimiter) net.Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &rateLimitedConn{
		ExtendedConn: bufio.NewExtendedConn(conn),
		ctx:          ctx,
		cancel:       cancel,
		limit:        limiter,
	}
}

func (c *rateLimitedConn) Read(p []byte) (n int, err error) {
	n, err = c.ExtendedConn.Read(p)
	if n > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, n))
	}
	return n, err
}

func (c *rateLimitedConn) ReadBuffer(buffer *buf.Buffer) error {
	err := c.ExtendedConn.ReadBuffer(buffer)
	if buffer.Len() > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, buffer.Len()))
	}
	return err
}

func (c *rateLimitedConn) Write(p []byte) (n int, err error) {
	n, err = c.ExtendedConn.Write(p)
	if n > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, n))
	}
	return n, err
}

func (c *rateLimitedConn) WriteBuffer(buffer *buf.Buffer) error {
	dataLen := buffer.Len()
	err := c.ExtendedConn.WriteBuffer(buffer)
	if dataLen > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, dataLen))
	}
	return err
}

func (c *rateLimitedConn) Close() error {
	c.cancel()
	return c.ExtendedConn.Close()
}

type rateLimitedPacketConn struct {
	N.PacketConn
	ctx    context.Context
	cancel context.CancelFunc
	limit  *rateLimiter
}

func newRateLimitedPacketConn(conn N.PacketConn, limiter *rateLimiter) N.PacketConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &rateLimitedPacketConn{
		PacketConn: conn,
		ctx:        ctx,
		cancel:     cancel,
		limit:      limiter,
	}
}

func (c *rateLimitedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	destination, err = c.PacketConn.ReadPacket(buffer)
	if buffer.Len() > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, buffer.Len()))
	}
	return destination, err
}

func (c *rateLimitedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	dataLen := buffer.Len()
	err := c.PacketConn.WritePacket(buffer, destination)
	if dataLen > 0 {
		err = mergeRateError(err, waitRate(c.ctx, c.limit, dataLen))
	}
	return err
}

func (c *rateLimitedPacketConn) Close() error {
	c.cancel()
	return c.PacketConn.Close()
}

func waitRate(ctx context.Context, limiter *rateLimiter, bytes int) error {
	if limiter == nil || bytes <= 0 {
		return nil
	}
	burst := limiter.Burst()
	if burst <= 0 {
		return nil
	}
	for bytes > 0 {
		chunk := bytes
		if chunk > burst {
			chunk = burst
		}
		if err := limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		bytes -= chunk
	}
	return nil
}

func mergeRateError(ioErr error, rateErr error) error {
	if ioErr != nil {
		return ioErr
	}
	return rateErr
}
