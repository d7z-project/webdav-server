package sftp_service

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"

	"code.d7z.net/packages/webdav-server/common"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SFTPServer struct {
	config *ssh.ServerConfig
}

func NewSFTPServer(ctx *common.FsContext) (*SFTPServer, error) {
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			_, err := ctx.LoadFS(conn.User(), "", key, false)
			if err != nil {
				slog.Warn("|security| Login failed.", "mode", "publicKey",
					"remote", conn.RemoteAddr().String(), "user", conn.User(), "key", string(key.Marshal()))
				return nil, err
			}
			return nil, nil
		},
	}
	if ctx.Config.SFTP.PasswordAuth {
		config.PasswordCallback = func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			_, err := ctx.LoadFS(conn.User(), string(password), nil, false)
			if err != nil {
				slog.Warn("|security| Login failed.", "mode", "password",
					"remote", conn.RemoteAddr().String(), "user", conn.User())
				return nil, err
			}
			return nil, nil
		}
	}
	for i, privatekey := range ctx.Config.SFTP.Privatekeys {
		key, err := ssh.ParsePrivateKey([]byte(privatekey))
		if err != nil {
			return nil, errors.Join(err, fmt.Errorf("failed to parse private key(%d): %s", i, privatekey))
		}
		config.AddHostKey(key)
	}
	return &SFTPServer{config: config}, nil
}

func (s *SFTPServer) Serve(ctx *common.FsContext, listener net.Listener) {
	go func() {
		<-ctx.Context().Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Context().Done():
				return
			default:
				slog.Error("Accept 错误", "err", err)
				continue
			}
		}
		go s.handler(ctx, conn)
	}
}

func (s *SFTPServer) handler(ctx *common.FsContext, conn net.Conn) {
	defer conn.Close()
	sConn, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			slog.Warn("failed to accept channel", "err", err)
			continue
		}
		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {
				switch req.Type {
				case "pty-req":
					_ = req.Reply(true, nil)
				case "shell":
					_ = req.Reply(true, nil)
					_, _ = fmt.Fprintf(channel, ctx.Config.SFTP.WelcomeMessage, sConn.User())
					_, _ = fmt.Fprintf(channel, "\r\nthis server only supports sftp file transfers.\r\n")
					_, _ = channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return
				case "subsystem":
					if string(req.Payload[4:]) == "sftp" {
						_ = req.Reply(true, nil)
						userFS := ctx.LoadUserFS(sConn.User())
						server := sftp.NewRequestServer(channel, FSHandlers(userFS))
						if err := server.Serve(); err != nil && err != io.EOF {
							slog.Warn("SFTP Server 错误", "err", err)
						}
						return
					}
					_ = req.Reply(false, nil)
				default:
					_ = req.Reply(false, nil)
				}
			}
		}(requests)
	}
}
