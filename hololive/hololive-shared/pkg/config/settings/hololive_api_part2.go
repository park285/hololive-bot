package settings

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func overlappingListenerOwner(parsed []listenerEndpoint, endpoint *listenerEndpoint) string {
	for index := range parsed {
		if endpointsOverlap(&parsed[index], endpoint) {
			return parsed[index].owner
		}
	}

	return ""
}

type listenerEndpoint struct {
	owner            string
	network          string
	addr             string
	host             string
	port             int
	expectedPort     int
	requirePortMatch bool
}

func parseListenerEndpoint(listener *listenerEndpoint) (listenerEndpoint, error) {
	host, port, err := splitListenerAddress(listener.addr)
	if err != nil {
		//nolint:wrapcheck // 하위 함수가 어떤 리스너의 무엇이 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return listenerEndpoint{}, err
	}

	if err := validateListenerPortMatch(listener, port); err != nil {
		//nolint:wrapcheck // 하위 함수가 어떤 리스너의 무엇이 잘못됐는지 담은 완결된 메시지를 만들므로, 다시 감싸면 같은 말이 반복된다.
		return listenerEndpoint{}, err
	}

	listener.host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	listener.port = port

	return *listener, nil
}

func splitListenerAddress(addr string) (host string, port int, err error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", 0, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	port, err = strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port in address %q", addr)
	}

	return host, port, nil
}

func validateListenerPortMatch(listener *listenerEndpoint, actualPort int) error {
	if !listener.requirePortMatch {
		return nil
	}

	if listener.expectedPort <= 0 || listener.expectedPort > 65535 {
		return errors.New("configured port must be between 1 and 65535")
	}

	if actualPort != listener.expectedPort {
		return fmt.Errorf("address port %d must match configured port %d", actualPort, listener.expectedPort)
	}

	return nil
}

func endpointsOverlap(left, right *listenerEndpoint) bool {
	return left.network == right.network && left.port == right.port && listenerHostsOverlap(left.host, right.host)
}

func listenerHostsOverlap(left, right string) bool {
	if isWildcardListenerHost(left) || isWildcardListenerHost(right) {
		return true
	}

	if left == right {
		return true
	}

	return listenerHostIPOverlap(left, right)
}

func listenerHostIPOverlap(left, right string) bool {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)

	if leftIP != nil && rightIP != nil {
		return leftIP.Equal(rightIP)
	}

	return listenerHostnameMatchesIP(left, rightIP) || listenerHostnameMatchesIP(right, leftIP)
}

func listenerHostnameMatchesIP(host string, ip net.IP) bool {
	return host == "localhost" && ip != nil && ip.IsLoopback()
}

func isWildcardListenerHost(host string) bool {
	if host == "" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsUnspecified()
}
