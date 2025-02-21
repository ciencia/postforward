package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"net/textproto"
	"os"
	"os/exec"
	"time"
)

// Exit codes as defined in <sysexits.h>
const (
	// The input data was incorrect in some way.  This
	// should only be used for user's data and not system
	// files.
	ExDataErr = 65
	// Temporary failure, indicating something that is not
	// really an error.  In sendmail, this means that a
	// mailer (e.g.) could not create a connection, and
	// the request should be reattempted later.
	ExTempFail = 75
)

var dryRun = flag.Bool("dry-run", false, "show what would be done, don't actually forward mail")
var path = flag.String("path", "", "override $PATH with this value when executing binaries")
var rpHeader = flag.String("rp-header", "Return-Path", "header name containing the return-path (MAIL FROM) value")
var sendmailPath = flag.String("sendmail-path", "sendmail", "path to the sendmail binary (deprecated: use --path instead)")
var srsAddr = flag.String("srs-addr", "localhost:10001", "TCP address for SRS lookups")

// lookupTCP performs a TCP table lookup for the specified key against the
// given address.
func lookupTCP(addr, key string) (string, error) {
	c, err := textproto.Dial("tcp", addr)
	if err != nil {
		return "", err
	}

	id, err := c.Cmd("get " + key)
	if err != nil {
		return "", err
	}
	c.StartResponse(id)
	defer c.EndResponse(id)

	code, msg, err := c.ReadCodeLine(-1)
	if err != nil {
		return "", err
	}
	switch code {
	case 200:
		return msg, nil
	case 500:
		fmt.Fprintf(os.Stderr, "warning: srs: returncode 500 (%v)\n", msg)
		return key, nil
	default:
		return "", fmt.Errorf("srs: unexpected returncode %d (%v)", code, msg)
	}
}

// die writes msg to stderr and aborts the program with the given status code.
func die(msg string, code int) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}

// headerRewriter wraps the given reader and performs header rewriting on read
// data. Specifically, this strips the "From sender time_stamp" envelope header
// inserted by Postfix and adds supplied headers.
//
// Note that the Return-Path header is left intact. Postfix (specifically,
// the cleanup daemon) will replace this header automatically.
func headerRewriter(in io.Reader, headers []string) io.Reader {
	buffer := bytes.Buffer{}
	reader := bufio.NewReader(in)
	linenum := 0
	for {
		linenum++
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				buffer.Write(line)
				return &buffer
			}
			die(fmt.Sprintf("Unexpected error occurred while reading input: %s", err), ExTempFail)
		}

		if linenum == 1 {
			lineEnding := guessLineEnding(line)
			for _, header := range headers {
				buffer.WriteString(header)
				buffer.Write(lineEnding)
			}

			if bytes.HasPrefix(line, []byte("From ")) {
				continue
			}
		}
		// Remove From: header in case it exists
		if bytes.HasPrefix(line, []byte("From: ")) {
			continue
		}
		buffer.Write(line)
	}
}

// getHostname returns the system hostname. It tries to get the value from
// postfix, falling back to os.Hostname() when that fails.
func getHostname() string {
	out, err := exec.Command("postconf", "-h", "myhostname").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: unable to get hostname from postfix (%v)\n", err)
		hostname, _ := os.Hostname()
		return hostname
	}
	return string(bytes.TrimSpace(out))
}

// guessLineEnding guesses the correct line endings to use based on the line
// ending used in the supplied input line. This mimics postfix behavior, as
// seen in sendmail.c:
//
// if (strip_cr == STRIP_CR_DUNNO && type == REC_TYPE_NORM) {
//     if (VSTRING_LEN(buf) > 0 && vstring_end(buf)[-1] == '\r')
//         strip_cr = STRIP_CR_DO;
//     else
//         strip_cr = STRIP_CR_DONT;
//
// Note that based on http://www.postfix.org/postconf.5.html#sendmail_fix_line_endings,
// we should be able to get away with hard-coding \r\n or \n as well.
func guessLineEnding(line []byte) []byte {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return []byte("\r\n")
	}
	return []byte("\n")
}

func main() {
	flag.Parse()
	if *path != "" {
		err := os.Setenv("PATH", *path)
		if err != nil {
			die(fmt.Sprintf("Unable to set $PATH: %s", err), ExTempFail)
		}
	}

	buffer := bytes.Buffer{}
	message, err := mail.ReadMessage(io.TeeReader(os.Stdin, &buffer))
	if err != nil {
		die(fmt.Sprintf("Parse error: %s", err), ExDataErr)
	}

	returnPath := message.Header.Get(*rpHeader)
	if returnPath == "" {
		die("Parse error: Missing return-path header in message", ExDataErr)
	}

	fromName := message.Header.Get("From")
	if fromName == "" {
		fromName = "unknown (forwarded)"
	} else {
		fromName = fromName + " (forwarded)"
	}

	extraHeaders := []string{
		fmt.Sprintf("Received: by %s (Postforward); %s",
			getHostname(), time.Now().Format("Mon, 2 Jan 2006 15:04:05 -0700")),
		fmt.Sprintf("X-Original-Return-Path: %s", returnPath)}

	returnPath = returnPath[1 : len(returnPath)-1] // Remove <> brackets
	returnPath, err = lookupTCP(*srsAddr, returnPath)
	if err != nil {
		die(fmt.Sprintf("SRS lookup error: %s", err), ExTempFail)
	}

	mailreader := io.MultiReader(headerRewriter(&buffer, extraHeaders), os.Stdin)
	args := append([]string{"-i", "-f", returnPath, "-F", fromName}, flag.Args()...)
	sendmail := exec.Command(*sendmailPath, args...)
	sendmail.Stdin = mailreader
	sendmail.Stdout = os.Stdout
	sendmail.Stderr = os.Stderr

	if *dryRun {
		fmt.Printf("Would call sendmail with args: %v\n", args)
		fmt.Print("Would pipe the following data into sendmail:\n\n")
		io.Copy(os.Stdout, mailreader)
		os.Exit(0)
	}

	if err = sendmail.Run(); err != nil {
		die(fmt.Sprintf("Error delivering message to sendmail: %s", err), ExTempFail)
	}

}
