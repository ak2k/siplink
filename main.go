package main

import (
	"context"
	"fmt"
	"log"
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

	phone1 := os.Args[1]
	phone2 := os.Args[2]

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

	// Initialize SIPGO
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("CallTransfer"))
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
		event := req.GetHeader("Event")
		log.Printf("📨 Received NOTIFY: %s", event)
		
		// Check the body for transfer status
		body := string(req.Body())
		log.Printf("📨 NOTIFY body: %s", body)
		
		// Decode the transfer status
		if strings.Contains(body, "SIP/2.0 200") {
			if strings.Contains(body, "OK") {
				log.Println("✅ Transfer ACCEPTED by server")
			}
		} else if strings.Contains(body, "SIP/2.0 180") {
			log.Println("📞 Target number is RINGING")
		} else if strings.Contains(body, "SIP/2.0 183") {
			log.Println("📡 Call is making PROGRESS")
		} else if strings.Contains(body, "SIP/2.0 486") {
			log.Println("❌ Target is BUSY")
		} else if strings.Contains(body, "SIP/2.0 404") {
			log.Println("❌ Target number NOT FOUND")
		} else if strings.Contains(body, "SIP/2.0 487") {
			log.Println("❌ Transfer CANCELLED")
		} else if strings.Contains(body, "SIP/2.0 603") {
			log.Println("❌ Transfer DECLINED")
		}
		
		// Look for successful transfer (target answered)
		if (strings.Contains(body, "SIP/2.0 200") && strings.Contains(body, "OK")) ||
		   strings.Contains(body, "SIP/2.0 180") {
			// Transfer is progressing or completed
			select {
			case transferComplete <- true:
			default:
			}
		}
		
		// Send 200 OK response
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

	// Create Dialog UA for calls
	contactHdr := sip.ContactHeader{
		Address: sip.Uri{User: username, Host: localIP, Port: 5060},
	}
	dialogUA := sipgo.DialogUA{
		Client:     client,
		ContactHDR: contactHdr,
	}

	// Make call
	dialog, err := makeCall(ctx, &dialogUA, phone1, server, username, password)
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
		log.Println("🎉 Application can exit - call continues on server")
	case <-time.After(30 * time.Second):
		log.Println("⏰ Transfer timeout - sending BYE to cleanup")
		dialog.Bye(ctx)
	}

	log.Println("\n✓ Call transfer completed successfully!")
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
	sip.ParseUri(fmt.Sprintf("sip:%s@%s", username, server), &recipient)
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.AppendHeader(
		sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s>", username, localIP)),
	)
	req.SetTransport("UDP")

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

func makeCall(ctx context.Context, dialogUA *sipgo.DialogUA, phoneNumber, server, username, password string) (*sipgo.DialogClientSession, error) {
	log.Printf("📞 Calling %s...", phoneNumber)

	recipient := sip.Uri{User: phoneNumber, Host: server}

	// Create SDP with G.722 HD voice preference
	sdp := fmt.Sprintf(`v=0
o=%s 123456 654321 IN IP4 %s
s=Call Transfer
c=IN IP4 %s
t=0 0
m=audio 10000 RTP/AVP 9 0 8
a=rtpmap:9 G722/8000
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=sendrecv
`, username, getLocalIP(), getLocalIP())

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
	select {
	case <-tx.Done():
		return nil, fmt.Errorf("transaction died")
	case res := <-tx.Responses():
		return res, nil
	}
}