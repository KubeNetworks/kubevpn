package core

import (
	"context"
	"errors"
	"github.com/wencaiwulue/kubevpn/remote"
	"github.com/wencaiwulue/kubevpn/util"
	"io"
	"net"
	"sync"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/shadowaead"
	log "github.com/sirupsen/logrus"
	"github.com/songgao/water/waterutil"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type tunRouteKey [16]byte

func ipToTunRouteKey(ip net.IP) (key tunRouteKey) {
	copy(key[:], ip.To16())
	return
}

type tunHandler struct {
	options *HandlerOptions
	routes  sync.Map
	chExit  chan struct{}
}

// TunHandler creates a handler for tun tunnel.
func TunHandler(opts ...HandlerOption) Handler {
	h := &tunHandler{
		options: &HandlerOptions{},
		chExit:  make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(h.options)
	}

	return h
}

func (h *tunHandler) Init(options ...HandlerOption) {
	if h.options == nil {
		h.options = &HandlerOptions{}
	}
	for _, opt := range options {
		opt(h.options)
	}
}

func (h *tunHandler) Handle(conn net.Conn) {
	//defer os.Exit(0)
	defer conn.Close()
	ctx, cancelFunc := context.WithCancel(context.TODO())
	remote.CancelFunctions = append(remote.CancelFunctions, cancelFunc)

	var err error
	var raddr net.Addr
	if addr := h.options.Node.Remote; addr != "" {
		raddr, err = net.ResolveUDPAddr("udp", addr)
		if err != nil {
			log.Debugf("[tun] %s: remote addr: %v", conn.LocalAddr(), err)
			return
		}
	}

	var tempDelay time.Duration
	for ctx.Err() == nil {
		err := func() error {
			var err error
			var pc net.PacketConn
			// fake tcp mode will be ignored when the client specifies a chain.
			if raddr != nil && !h.options.Chain.IsEmpty() {
				cc, err := h.options.Chain.DialContext(context.Background(), "udp", raddr.String())
				if err != nil {
					return err
				}
				var ok bool
				pc, ok = cc.(net.PacketConn)
				if !ok {
					err = errors.New("not a packet connection")
					log.Debugf("[tun] %s - %s: %s", conn.LocalAddr(), raddr, err)
					return err
				}
			} else {
				laddr, _ := net.ResolveUDPAddr("udp", h.options.Node.Addr)
				pc, err = net.ListenUDP("udp", laddr)
			}
			if err != nil {
				return err
			}

			return h.transportTun(conn, pc, raddr)
		}()
		if err != nil {
			log.Debugf("[tun] %s: %v", conn.LocalAddr(), err)
		}

		select {
		case <-h.chExit:
			return
		case <-ctx.Done():
			h.chExit <- struct{}{}
		default:
		}

		if err != nil {
			if tempDelay == 0 {
				tempDelay = 1000 * time.Millisecond
			} else {
				tempDelay *= 2
			}
			if max := 6 * time.Second; tempDelay > max {
				tempDelay = max
			}
			time.Sleep(tempDelay)
			continue
		}
		tempDelay = 0
	}
}

func (h *tunHandler) findRouteFor(dst net.IP) net.Addr {
	if v, ok := h.routes.Load(ipToTunRouteKey(dst)); ok {
		return v.(net.Addr)
	}
	for _, route := range h.options.IPRoutes {
		if route.Dest.Contains(dst) && route.Gateway != nil {
			if v, ok := h.routes.Load(ipToTunRouteKey(route.Gateway)); ok {
				return v.(net.Addr)
			}
		}
	}
	return nil
}

func (h *tunHandler) transportTun(tun net.Conn, conn net.PacketConn, raddr net.Addr) error {
	errc := make(chan error, 1)
	ctx, cancelFunc := context.WithCancel(context.Background())
	remote.CancelFunctions = append(remote.CancelFunctions, cancelFunc)
	go func() {
		for ctx.Err() == nil {
			err := func() error {
				b := util.SPool.Get().([]byte)
				defer util.SPool.Put(b)

				n, err := tun.Read(b)
				if err != nil {
					select {
					case h.chExit <- struct{}{}:
					default:
					}
					return err
				}

				var src, dst net.IP
				if waterutil.IsIPv4(b[:n]) {
					header, err := ipv4.ParseHeader(b[:n])
					if err != nil {
						log.Debugf("[tun] %s: %v", tun.LocalAddr(), err)
						return nil
					}
					if util.Debug {
						log.Debugf("[tun] %s", header.String())
					}
					src, dst = header.Src, header.Dst
				} else if waterutil.IsIPv6(b[:n]) {
					header, err := ipv6.ParseHeader(b[:n])
					if err != nil {
						log.Debugf("[tun] %s: %v", tun.LocalAddr(), err)
						return nil
					}
					if util.Debug {
						log.Debugf("[tun] %s", header.String())
					}
					src, dst = header.Src, header.Dst
				} else {
					log.Debugf("[tun] unknown packet")
					return nil
				}

				// client side, deliver packet directly.
				if raddr != nil {
					_, err := conn.WriteTo(b[:n], raddr)
					return err
				}

				addr := h.findRouteFor(dst)
				if addr == nil {
					log.Debugf("[tun] no route for %s -> %s", src, dst)
					return nil
				}

				if util.Debug {
					log.Debugf("[tun] find route: %s -> %s", dst, addr)
				}
				if _, err := conn.WriteTo(b[:n], addr); err != nil {
					return err
				}
				return nil
			}()

			if err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		for ctx.Err() == nil {
			err := func() error {
				b := util.SPool.Get().([]byte)
				defer util.SPool.Put(b)

				n, addr, err := conn.ReadFrom(b)
				if err != nil &&
					err != shadowaead.ErrShortPacket {
					return err
				}

				var src, dst net.IP
				if waterutil.IsIPv4(b[:n]) {
					header, err := ipv4.ParseHeader(b[:n])
					if err != nil {
						log.Debugf("[tun] %s: %v", tun.LocalAddr(), err)
						return nil
					}
					if util.Debug {
						log.Debugf("[tun] %s", header.String())
					}
					src, dst = header.Src, header.Dst
				} else if waterutil.IsIPv6(b[:n]) {
					header, err := ipv6.ParseHeader(b[:n])
					if err != nil {
						log.Debugf("[tun] %s: %v", tun.LocalAddr(), err)
						return nil
					}
					if util.Debug {
						log.Debugf("[tun] %s", header.String())
					}
					src, dst = header.Src, header.Dst
				} else {
					log.Debugf("[tun] unknown packet")
					return nil
				}

				// client side, deliver packet to tun device.
				if raddr != nil {
					_, err := tun.Write(b[:n])
					return err
				}

				rkey := ipToTunRouteKey(src)
				if actual, loaded := h.routes.LoadOrStore(rkey, addr); loaded {
					if actual.(net.Addr).String() != addr.String() {
						log.Debugf("[tun] update route: %s -> %s (old %s)",
							src, addr, actual.(net.Addr))
						h.routes.Store(rkey, addr)
					}
				} else {
					log.Debugf("[tun] new route: %s -> %s", src, addr)
				}

				if addr := h.findRouteFor(dst); addr != nil {
					if util.Debug {
						log.Debugf("[tun] find route: %s -> %s", dst, addr)
					}
					_, err := conn.WriteTo(b[:n], addr)
					return err
				}

				if _, err := tun.Write(b[:n]); err != nil {
					select {
					case h.chExit <- struct{}{}:
					default:
					}
					return err
				}
				return nil
			}()

			if err != nil {
				errc <- err
				return
			}
		}
	}()

	select {
	case err := <-errc:
		if err != nil && err == io.EOF {
			err = nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
	//err := <-errc
	//if err != nil && err == io.EOF {
	//	err = nil
	//}
	//return err
}
