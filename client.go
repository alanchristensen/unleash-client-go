package unleash_client_go

import (
	"fmt"
	"github.com/unleash/unleash-client-go/context"
	s "github.com/unleash/unleash-client-go/internal/strategies"
	"github.com/unleash/unleash-client-go/strategy"
	"net/url"
	"strings"
	"time"
)

const deprecatedSuffix = "/features"

var defaultStrategies = []strategy.Strategy{
	*s.NewDefaultStrategy(),
	*s.NewApplicationHostnameStrategy(),
	*s.NewGradualRolloutRandomStrategy(),
	*s.NewGradualRolloutSessionId(),
	*s.NewGradualRolloutUserId(),
	*s.NewRemoteAddressStrategy(),
	*s.NewUserWithIdStrategy(),
}

type featureOption struct {
	fallback *bool
	ctx      *context.Context
}

type FeatureOption func(*featureOption)

func WithFallback(fallback bool) FeatureOption {
	return func(opts *featureOption) {
		opts.fallback = &fallback
	}
}

func WithContext(ctx context.Context) FeatureOption {
	return func(opts *featureOption) {
		opts.ctx = &ctx
	}
}

type Client struct {
	errorEmitterImpl
	options    configOption
	repository *repository
	strategies []strategy.Strategy
	ready      chan bool
}

func NewClient(options ...ConfigOption) (*Client, error) {
	uc := &Client{
		errorEmitterImpl: *newErrorEmitter(),
		options: configOption{
			refreshInterval: 15 * time.Second,
			metricsInterval: 60 * time.Second,
			disableMetrics:  false,
			backupPath:      getTmpDirPath(),
			strategies:      []strategy.Strategy{},
		},
	}

	for _, opt := range options {
		opt(&uc.options)
	}

	if uc.options.url == "" {
		return nil, fmt.Errorf("Unleash server URL missing")
	}

	if strings.HasSuffix(uc.options.url, deprecatedSuffix) {
		uc.warn(fmt.Errorf("Unleash server URL %s should no longer link directly to /features", uc.options.url))
		uc.options.url = strings.TrimSuffix(uc.options.url, deprecatedSuffix)
	}

	if !strings.HasSuffix(uc.options.url, "/") {
		uc.options.url += "/"
	}

	parsedUrl, err := url.Parse(uc.options.url)
	if err != nil {
		return nil, err
	}

	if uc.options.appName == "" {
		return nil, fmt.Errorf("Unleash client appName missing")
	}

	if uc.options.instanceId == "" {
		uc.options.instanceId = generateInstanceId()
	}

	uc.repository = NewRepository(RepositoryOptions{
		BackupPath:      uc.options.backupPath,
		Url:             *parsedUrl,
		AppName:         uc.options.appName,
		InstanceId:      uc.options.instanceId,
		RefreshInterval: uc.options.refreshInterval,
	})

	uc.repository.Forward(uc)

	uc.strategies = append(defaultStrategies, uc.options.strategies...)

	return uc, nil

}

func (uc Client) IsEnabled(feature string, options ...FeatureOption) bool {
	f := uc.repository.GetToggle(feature)

	var opts featureOption
	for _, o := range options {
		o(&opts)
	}

	if f == nil {
		if opts.fallback != nil {
			return *opts.fallback
		}
		return false
	}

	if !f.Enabled {
		return false
	}

	for _, s := range f.Strategies {
		foundStrategy := uc.getStrategy(s.Name)
		if foundStrategy == nil {
			// TODO: warnOnce missingStrategy
			continue
		}
		return foundStrategy.IsEnabled(f.Parameters, opts.ctx)
	}
	return false
}

func (uc *Client) Close() error {
	uc.repository.Close()
	return nil
}

func (uc Client) Ready() <-chan bool {
	return uc.ready
}

func (uc Client) getStrategy(name string) strategy.Strategy {
	for _, strategy := range uc.strategies {
		if strategy.Name() == name {
			return strategy
		}
	}
	return nil
}
