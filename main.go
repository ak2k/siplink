package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
)

func showHelp() {
	fmt.Println("SIPLink - SIP Call Bridging Tool")
	fmt.Println("================================")
	fmt.Println("\nUsage: siplink <phone1> <phone2>")
	fmt.Println("\nExample: siplink 15551234567 15559876543")
	fmt.Println("\nSet environment variables:")
	fmt.Println("  export VOIPMS_USER='your_account'")
	fmt.Println("  export VOIPMS_PASS='your_password'")
	fmt.Println("  export VOIPMS_SERVER='chicago.voip.ms'")
	fmt.Println("\nSee https://github.com/ak2k/siplink for more info.")
}

func main() {
	// Check for help flag
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		showHelp()
		os.Exit(0)
	}

	if len(os.Args) != 3 {
		showHelp()
		os.Exit(1)
	}

	phone1 := normalizePhone(os.Args[1])
	phone2 := normalizePhone(os.Args[2])

	username := os.Getenv("VOIPMS_USER")
	password := os.Getenv("VOIPMS_PASS")
	server := os.Getenv("VOIPMS_SERVER")

	if username == "" || password == "" {
		log.Fatal("Error: Set VOIPMS_USER and VOIPMS_PASS")
	}
	if server == "" {
		server = "chicago.voip.ms"
	}

	fmt.Printf("Call Transfer: %s → %s\n", phone1, phone2)
	fmt.Printf("Server: %s@%s\n", username, server)
	fmt.Println("========================================")

	ctx := context.Background()

	// Initialize SIPGO with TLS
	// VoIP.ms requires non-PFS cipher suites for TLS 1.2
	tlsConfig := &tls.Config{
		ServerName: server,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("CallTransfer"),
		sipgo.WithUserAgenTLSConfig(tlsConfig),
	)
	if err != nil {
		log.Fatalf("Failed to create UA: %v", err)
	}

	localIP := getLocalIP()
	
	// Create server to handle incoming NOTIFY requests
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Close()

	transferComplete := make(chan bool, 1)

	// Handle NOTIFY requests for transfer status
	srv.OnRequest(sip.NOTIFY, func(req *sip.Request, tx sip.ServerTransaction) {
		body := string(req.Body())
		log.Printf("📨 NOTIFY: %s", strings.TrimSpace(body))

		if strings.Contains(body, "SIP/2.0 200") {
			select {
			case transferComplete <- true:
			default:
			}
		}

		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		if err := tx.Respond(res); err != nil {
			log.Printf("Failed to respond to NOTIFY: %v", err)
		}
	})

	// Handle BYE requests  
	srv.OnRequest(sip.BYE, func(req *sip.Request, tx sip.ServerTransaction) {
		log.Printf("📨 Received BYE from server - call ended")
		
		// Send 200 OK response
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		if err := tx.Respond(res); err != nil {
			log.Printf("Failed to respond to BYE: %v", err)
		}
	})

	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(localIP))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	log.Println("✓ SIP client initialized")

	// Register to SIP server
	if err := registerToServer(ctx, client, username, password, server, localIP); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	// Start TLS listener for incoming requests (NOTIFY, BYE)
	listenerTLS := &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert()},
	}
	go func() {
		if err := srv.ListenAndServeTLS(ctx, "tls", localIP+":5061", listenerTLS); err != nil {
			log.Printf("TLS listener error: %v", err)
		}
	}()

	// Create Dialog UA for calls
	contactHdr := sip.ContactHeader{
		Address: sip.Uri{User: username, Host: localIP, Port: 5061},
	}
	dialogUA := sipgo.DialogUA{
		Client:     client,
		ContactHDR: contactHdr,
	}

	// Make call
	dialog, err := makeCall(ctx, &dialogUA, phone1, server, localIP, username, password)
	if err != nil {
		log.Fatalf("Failed to make call: %v", err)
	}
	defer dialog.Close()

	// Transfer call immediately
	if err := transferCall(ctx, dialog, phone2, server); err != nil {
		log.Fatalf("Failed to transfer call: %v", err)
	}

	// Wait for transfer to complete or timeout
	log.Println("⏳ Waiting for transfer to complete...")
	
	select {
	case <-transferComplete:
		log.Println("✅ Transfer successful - calls are now connected!")
	case <-time.After(30 * time.Second):
		log.Println("⏰ Transfer timeout - sending BYE to cleanup")
		dialog.Bye(ctx)
		os.Exit(1)
	}
}

func normalizePhone(number string) string {
	// Strip common formatting
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, number)
	// Prepend country code for 10-digit US/Canada numbers
	if len(cleaned) == 10 {
		cleaned = "1" + cleaned
	}
	return cleaned
}

func selfSignedCert() tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func registerToServer(ctx context.Context, client *sipgo.Client, username, password, server, localIP string) error {
	log.Printf("Registering to %s...", server)

	recipient := sip.Uri{}
	sip.ParseUri(fmt.Sprintf("sips:%s@%s:5061", username, server), &recipient)
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.AppendHeader(
		sip.NewHeader("Contact", fmt.Sprintf("<sips:%s@%s:5061>", username, localIP)),
	)
	req.SetTransport("TLS")

	tx, err := client.TransactionRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send REGISTER: %w", err)
	}
	defer tx.Terminate()

	res, err := getResponse(tx)
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	if res.StatusCode == 401 {
		return authenticateAndRegister(ctx, client, req, res, username, password, recipient)
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("registration failed: %d %s", res.StatusCode, res.Reason)
	}

	log.Println("✓ Registered successfully")
	return nil
}

func authenticateAndRegister(ctx context.Context, client *sipgo.Client, originalReq *sip.Request, challenge *sip.Response, username, password string, recipient sip.Uri) error {
	wwwAuth := challenge.GetHeader("WWW-Authenticate")
	chal, err := digest.ParseChallenge(wwwAuth.Value())
	if err != nil {
		return fmt.Errorf("failed to parse challenge: %w", err)
	}

	cred, err := digest.Digest(chal, digest.Options{
		Method:   originalReq.Method.String(),
		URI:      recipient.Host,
		Username: username,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("failed to create digest: %w", err)
	}

	newReq := originalReq.Clone()
	newReq.RemoveHeader("Via")
	newReq.AppendHeader(sip.NewHeader("Authorization", cred.String()))

	tx, err := client.TransactionRequest(ctx, newReq)
	if err != nil {
		return fmt.Errorf("failed to send authenticated REGISTER: %w", err)
	}
	defer tx.Terminate()

	res, err := getResponse(tx)
	if err != nil {
		return fmt.Errorf("failed to get authenticated response: %w", err)
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("authenticated registration failed: %d %s", res.StatusCode, res.Reason)
	}

	log.Println("✓ Registered successfully")
	return nil
}

func makeCall(ctx context.Context, dialogUA *sipgo.DialogUA, phoneNumber, server, localIP, username, password string) (*sipgo.DialogClientSession, error) {
	log.Printf("📞 Calling %s...", phoneNumber)

	recipient := sip.Uri{User: phoneNumber, Host: server, Port: 5061, UriParams: sip.HeaderParams{{K: "transport", V: "tls"}}}

	// Generate random SRTP key (30 bytes, base64-encoded = 40 chars)
	srtpKey := make([]byte, 30)
	if _, err := rand.Read(srtpKey); err != nil {
		return nil, fmt.Errorf("failed to generate SRTP key: %w", err)
	}

	// Create SDP with G.722 HD voice over SRTP
	sdp := fmt.Sprintf(`v=0
o=%s 123456 654321 IN IP4 %s
s=Call Transfer
c=IN IP4 %s
t=0 0
m=audio 10000 RTP/SAVP 9 101
a=rtpmap:9 G722/8000
a=rtpmap:101 telephone-event/8000
a=fmtp:101 0-16
a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:%s
a=sendrecv
a=ptime:20
`, username, localIP, localIP, base64.StdEncoding.EncodeToString(srtpKey))

	contentTypeHeader := sip.NewHeader("Content-Type", "application/sdp")

	dialog, err := dialogUA.Invite(ctx, recipient, []byte(sdp), contentTypeHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to send INVITE: %w", err)
	}

	// Wait for answer with authentication support
	err = dialog.WaitAnswer(ctx, sipgo.AnswerOptions{
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, fmt.Errorf("call failed: %w", err)
	}

	log.Println("✓ Call connected!")

	// Send ACK
	if err := dialog.Ack(ctx); err != nil {
		return nil, fmt.Errorf("failed to send ACK: %w", err)
	}

	return dialog, nil
}

func transferCall(ctx context.Context, dialog *sipgo.DialogClientSession, targetNumber, server string) error {
	log.Printf("📞 Transferring call to %s...", targetNumber)

	referTo := fmt.Sprintf("sip:%s@%s", targetNumber, server)
	log.Printf("📤 REFER-TO: %s", referTo)
	
	recipient := dialog.InviteRequest.Recipient
	log.Printf("📤 REFER recipient: %s", recipient.String())
	
	refer := sip.NewRequest(sip.REFER, recipient)
	refer.AppendHeader(sip.NewHeader("Refer-To", referTo))
	refer.AppendHeader(sip.NewHeader("Referred-By", 
		fmt.Sprintf("sip:%s@%s", dialog.InviteRequest.From().Address.User, server)))

	tx, err := dialog.TransactionRequest(ctx, refer)
	if err != nil {
		return fmt.Errorf("failed to send REFER: %w", err)
	}
	defer tx.Terminate()

	res, err := getResponse(tx)
	if err != nil {
		return fmt.Errorf("failed to get REFER response: %w", err)
	}

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		log.Println("✓ Transfer initiated!")
		return nil
	}

	return fmt.Errorf("transfer failed: %d %s", res.StatusCode, res.Reason)
}

func getResponse(tx sip.ClientTransaction) (*sip.Response, error) {
	for {
		select {
		case <-tx.Done():
			return nil, fmt.Errorf("transaction completed without final response")
		case res := <-tx.Responses():
			if res.StatusCode >= 200 {
				return res, nil
			}
		}
	}
}