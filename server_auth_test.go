package main

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestSocks5AuthClashHandshake(t *testing.T) {
	username = "admin"
	password = "admin"
	skipPrivateCheck = false

	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Socks5Auth(server)
	}()

	// Clash/mihomo: VER=5, methods none+password
	if _, err := client.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
		t.Fatal(err)
	}
	sel := make([]byte, 2)
	if _, err := io.ReadFull(client, sel); err != nil {
		t.Fatal(err)
	}
	if sel[0] != 0x05 || sel[1] != 0x02 {
		t.Fatalf("method select = %v, want 05 02", sel)
	}

	// RFC 1929 subnegotiation VER=0x01
	req := []byte{0x01, 0x05}
	req = append(req, []byte("admin")...)
	req = append(req, 0x05)
	req = append(req, []byte("admin")...)
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	authRsp := make([]byte, 2)
	if _, err := io.ReadFull(client, authRsp); err != nil {
		t.Fatal(err)
	}
	if authRsp[0] != 0x01 || authRsp[1] != 0x00 {
		t.Fatalf("auth reply = %v, want 01 00 (Clash rejects 05 00)", authRsp)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auth timed out")
	}
}

func TestUDPRelayUsesFixedPort(t *testing.T) {
	if err := startUDPRelay(0); err != nil {
		t.Fatal(err)
	}
	defer socksUDP.conn.Close()
	relayPort := socksUDP.port

	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := echo.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteToUDP(buf[:n], addr)
		}
	}()

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess := socksUDP.track(net.IPv4(127, 0, 0, 1))
	defer sess.close()

	echoAddr := echo.LocalAddr().(*net.UDPAddr)
	pkt := buildUDPDatagram(echoAddr, []byte("ping"))
	if _, err := client.WriteToUDP(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: relayPort}); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	payload, src, err := parseUDPDatagram(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "ping" {
		t.Fatalf("payload %q", payload)
	}
	if src.Port != echoAddr.Port {
		t.Fatalf("reply from %s, echo is %s", src, echoAddr)
	}
}

func TestSocks5ConnectUnsupportedCmdReplies07(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		_, _, _, err := Socks5Connect(server)
		errCh <- err
	}()

	// BIND is unsupported; send header only so the reply is not blocked on leftover bytes.
	if _, err := client.Write([]byte{0x05, 0x02, 0x00, 0x01}); err != nil {
		t.Fatal(err)
	}
	rsp := make([]byte, 10)
	if _, err := io.ReadFull(client, rsp); err != nil {
		t.Fatal(err)
	}
	if rsp[0] != 0x05 || rsp[1] != 0x07 {
		t.Fatalf("connect reply = %v, want VER=5 REP=7", rsp)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected connect error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("connect timed out")
	}
}
