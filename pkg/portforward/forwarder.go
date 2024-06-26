// Package portforward provides a TCP forwarder implementation.
package portforward

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

// Forwarder forwards connections from a source listener to a target address.
//
// The zero value for Forwarder is a valid configuration that forwards incoming
// connections to :http (localhost port 80).
type Forwarder struct {
	// TargetAddr specifies the TCP address to forward incoming connections
	// to, in the form "host:port". If empty, ":http" (port 80) is used.
	// See net.Dial for details of the address format.
	TargetAddr string

	// ErrorLog specifies an optional logger for errors accepting
	// connections and errors while forwarding connections. If nil,
	// logging is done via the log package's standard logger.
	ErrorLog *log.Logger

	lis *onceCloseListener
}

func (f *Forwarder) String() string {
	return f.TargetAddr
}

// Open accepts incoming connections on l, creating a new service goroutine for
// each. The service goroutines open a new connection to f.TargetAddr and
// forward the data read from the incoming connection.
//
// Open always closes l before returning. Any non-retryable error that occurs
// while accepting connections will be returned. Errors occurring while
// forwarding an accepted connections won't cause Open to return. They are
// logged using f.ErrorLog. If a Close causes the forwarder to stop and Open to
// return, nil will be returned.
func (f *Forwarder) Open(l net.Listener) error {
	if InterfaceIsNil(l) {
		return fmt.Errorf("nil listener")
	}
	
	f.lis = &onceCloseListener{Listener: l}
	defer l.Close()

	target := f.TargetAddr
	if target == "" {
		target = ":http"
	}

	// Waits for all connection handlers to finish.
	var handlers sync.WaitGroup

	// Loop until f.lis is closed.
	for {
		// f.lis.Accept waits for new connections. Unblocks with an
		// io.EOF error if f.lis.Close is called. Earlier accepted
		// connections can still finish.
		conn, err := f.lis.Accept()
		if err != nil {
			// Any net package errors that are assured to be
			// retry-able will conform to the net.Error interface,
			// and return Temporary true.
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				f.logf("accepting conn temporary error: %v\n", err)
				continue
			}
			if err != io.EOF {
				f.logf("accepting conn fatal error: %v\n", err)
				f.lis.Close()
			}
			handlers.Wait()
			return err
		}

		// Handle connection.
		handlers.Add(1)
		go func() {
			err := f.handleConnection(conn, target)
			if err != nil {
				f.logf("error forwarding connection: %v\n", err)
			}
			conn.Close()
			handlers.Done()
		}()
	}
}

func (f *Forwarder) handleConnection(conn net.Conn, target string) error {
	// Open connection to forwarder target.
	targetConn, err := net.Dial("tcp", target)
	if err != nil {
		// TODO(fischor): Close the forwarder in case this is a
		// non-retryable error?
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		_, err := io.Copy(conn, targetConn)
		if err != nil {
			f.logf("error forwarding from source to target: %v", err)
		}
		wg.Done()
	}()
	go func() {
		_, err := io.Copy(targetConn, conn)
		if err != nil {
			f.logf("error forwarding from source to target: %v\n", err)
		}
		wg.Done()
	}()

	wg.Wait()

	return targetConn.Close()
}

func (f *Forwarder) logf(format string, args ...interface{}) {
	if f.ErrorLog != nil {
		f.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// Close closes the forwarder gracefully, stopping it from accepting new
// connections without interrupting any active connections.
//
// Close will close the forwarders source listener and return immediately. It
// propagates any error from the listeners Close call.
//
// When Close is called, Open does not return immediately. It will finish
// handling all active connections before returning.
func (f *Forwarder) Close() error {
	if f.lis != nil {
		return f.lis.Close()
	}
	return nil
}

// onceCloseListener wraps a net.Listener, protecting it from
// multiple Close calls.
type onceCloseListener struct {
	net.Listener
	once     sync.Once
	closeErr error
}

func (oc *onceCloseListener) Close() error {
	oc.once.Do(oc.close)
	return oc.closeErr
}

func (oc *onceCloseListener) close() { oc.closeErr = oc.Listener.Close() }
