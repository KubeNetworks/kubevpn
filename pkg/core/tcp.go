package core

import (
	"crypto/tls"
	"net"

	"github.com/wencaiwulue/kubevpn/pkg/config"
)

type tcpTransporter struct{}

func TCPTransporter() Transporter {
	return &tcpTransporter{}
}

func (tr *tcpTransporter) Dial(addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: config.DialTimeout}
	return tls.DialWithDialer(dialer, "tcp", addr, config.TlsConfigClient)
}

func TCPListener(addr string) (net.Listener, error) {
	laddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		return nil, err
	}
	return &tcpKeepAliveListener{ln}, nil
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln *tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	conn, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	_ = conn.SetKeepAlive(true)
	_ = conn.SetKeepAlivePeriod(config.KeepAliveTime)
	return conn, nil
}
