package bootstrap

import (
	"errors"
	"io"
	"testing"
)

type botOwnerCloser func() error

func (f botOwnerCloser) Close() error { return f() }

func TestBotInfrastructureOwnerPartialBuildClosesAcquiredResourcesAndPreservesErrors(t *testing.T) {
	clientErr := errors.New("client close failed")
	irisErr := errors.New("Iris close failed")

	var internalCloses, irisCloses int

	owner := &botInfrastructureOwner{
		internalClients: []io.Closer{botOwnerCloser(func() error { internalCloses++; return clientErr })},
		irisClient:      botOwnerCloser(func() error { irisCloses++; return irisErr }),
	}

	for range 2 {
		err := owner.Close()
		if !errors.Is(err, clientErr) || !errors.Is(err, irisErr) {
			t.Fatalf("Close error=%v want both client and Iris causes", err)
		}
	}

	if internalCloses != 1 || irisCloses != 1 {
		t.Fatalf("cleanup counts client/Iris=%d/%d want=1/1", internalCloses, irisCloses)
	}
}
