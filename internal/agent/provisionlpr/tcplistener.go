package provisionlpr

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TCPServer listens on a TCP port for XML event streams sent by Provision-ISR cameras in Alarm Server mode.
type TCPServer struct {
	addr     string
	receiver *Receiver
	log      *slog.Logger
	listener net.Listener
	closed   atomic.Bool
	wg       sync.WaitGroup
}

// NewTCPServer creates a new TCPServer.
func NewTCPServer(addr string, receiver *Receiver, log *slog.Logger) (*TCPServer, error) {
	if log == nil {
		log = slog.Default()
	}
	return &TCPServer{
		addr:     addr,
		receiver: receiver,
		log:      log,
	}, nil
}

// Start begins listening and serving connections.
func (s *TCPServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

// Addr returns the listener's network address.
func (s *TCPServer) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Close stops the TCP server and waits for connections to close.
func (s *TCPServer) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	s.wg.Wait()
	return err
}

func (s *TCPServer) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			s.log.Warn("provision-isr tcp accept error", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *TCPServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close() }()

	clientIP := ""
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		clientIP = tcpAddr.IP.String()
	} else {
		host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		clientIP = host
	}
	s.log.Info("provision-isr tcp connection accepted", "ip", clientIP)

	buf := make([]byte, 0, 64*1024)
	chunk := make([]byte, 4096)

	for {
		if s.closed.Load() {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, err := conn.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			for {
				startIdx := bytes.Index(buf, []byte("<?xml"))
				if startIdx == -1 {
					// Discard leading bytes if no <?xml found to prevent unbounded memory growth
					if len(buf) > 64*1024 {
						buf = buf[:0]
					}
					break
				}
				endIdx := bytes.Index(buf[startIdx:], []byte("</config>"))
				if endIdx == -1 {
					// Incomplete XML; wait for more chunks
					break
				}
				endIdx += startIdx + len("</config>")
				xmlBlock := buf[startIdx:endIdx]
				buf = buf[endIdx:]

				preview := string(bytes.TrimSpace(xmlBlock))
				if len(preview) > 160 {
					preview = preview[:160] + "..."
				}
				s.log.Info("provision-isr tcp xml block received", "ip", clientIP, "size", len(xmlBlock), "preview", preview)

				now := time.Now()
				if _, err := s.receiver.HandleRawEvent(clientIP, xmlBlock, now); err != nil {
					s.log.Warn("provision-isr tcp event handle error", "ip", clientIP, "error", err)
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !s.closed.Load() {
				s.log.Debug("provision-isr tcp connection closed", "ip", clientIP, "reason", err)
			}
			return
		}
	}
}
