package bootstrap

import (
	"fmt"
	"log/slog"
	"os"

	brokerclient "github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/pkg/nodepki"
)

// remoteBrokerClient builds the client for a broker on another node (docs/27).
//
// Every failure here returns an error rather than falling back to the local
// socket. That matters more than it looks: a panel configured for node B that
// quietly reverted to the broker on node A would run privileged operations —
// creating users, writing web server configs, restarting services — on the wrong
// machine, and would look perfectly healthy while doing it.
func remoteBrokerClient(cfg config.Config, log *slog.Logger) (*brokerclient.Client, error) {
	r := cfg.Broker.Remote
	if err := r.Validate(); err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(r.CAFile)
	if err != nil {
		return nil, fmt.Errorf("broker.remote.ca_file: %w", err)
	}
	certPEM, err := os.ReadFile(r.CertFile)
	if err != nil {
		return nil, fmt.Errorf("broker.remote.cert_file: %w", err)
	}
	keyPEM, err := os.ReadFile(r.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("broker.remote.key_file: %w", err)
	}
	tlsCfg, err := nodepki.ClientTLS(certPEM, keyPEM, caPEM, r.EffectiveServerName())
	if err != nil {
		return nil, err
	}
	return brokerclient.NewTLSClient(r.Addr, cfg.Broker.Token, tlsCfg, log)
}
