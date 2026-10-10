package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
)

// Legacy callers retain their SMTP behavior; bounded notifications use a socket
// deadline and close the connection on cancellation, including during TLS/SMTP.
func newSMTPClientContext(ctx context.Context, addr string) (*smtp.Client, func(), error) {
	deadline, bounded := ctx.Deadline()
	if !bounded {
		client, err := newSMTPClient(addr)
		return client, func() {}, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	cleanup := func() { stop() }
	smtpConn := conn
	if SMTPSSLEnabled || (SMTPPort == 465 && !SMTPStartTLSEnabled) {
		tlsConn := tls.Client(conn, smtpTLSConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			cleanup()
			_ = conn.Close()
			return nil, nil, err
		}
		smtpConn = tlsConn
	}
	client, err := smtp.NewClient(smtpConn, SMTPServer)
	if err != nil {
		cleanup()
		_ = conn.Close()
		return nil, nil, err
	}
	if SMTPStartTLSEnabled && smtpConn == conn {
		supported, _ := client.Extension("STARTTLS")
		if !supported {
			err = fmt.Errorf("SMTP server does not support STARTTLS")
		} else {
			err = client.StartTLS(smtpTLSConfig())
		}
		if err != nil {
			cleanup()
			_ = client.Close()
			return nil, nil, err
		}
	}
	return client, cleanup, nil
}
