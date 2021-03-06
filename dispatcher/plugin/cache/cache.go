//     Copyright (C) 2020-2021, IrineSistiana
//
//     This file is part of mosdns.
//
//     mosdns is free software: you can redistribute it and/or modify
//     it under the terms of the GNU General Public License as published by
//     the Free Software Foundation, either version 3 of the License, or
//     (at your option) any later version.
//
//     mosdns is distributed in the hope that it will be useful,
//     but WITHOUT ANY WARRANTY; without even the implied warranty of
//     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//     GNU General Public License for more details.
//
//     You should have received a copy of the GNU General Public License
//     along with this program.  If not, see <https://www.gnu.org/licenses/>.
package cache

import (
	"context"
	"fmt"
	"github.com/IrineSistiana/mosdns/dispatcher/handler"
	"github.com/IrineSistiana/mosdns/dispatcher/pkg/cache"
	"github.com/IrineSistiana/mosdns/dispatcher/pkg/dnsutils"
	"github.com/IrineSistiana/mosdns/dispatcher/pkg/utils"
	"github.com/miekg/dns"
	"go.uber.org/zap"
	"time"
)

const (
	PluginType = "cache"

	maxTTL uint32 = 3600 * 24 * 7 // one week
)

func init() {
	handler.RegInitFunc(PluginType, Init, func() interface{} { return new(Args) })

	handler.MustRegPlugin(preset(handler.NewBP("_default_cache", PluginType), &Args{}), true)
}

var _ handler.ESExecutablePlugin = (*cachePlugin)(nil)
var _ handler.ContextPlugin = (*cachePlugin)(nil)

type Args struct {
	Size            int    `yaml:"size"`
	CleanerInterval int    `yaml:"cleaner_interval"`
	Redis           string `yaml:"redis"`
}

type cachePlugin struct {
	*handler.BP
	args *Args

	c cache.DnsCache
}

func Init(bp *handler.BP, args interface{}) (p handler.Plugin, err error) {
	return newCachePlugin(bp, args.(*Args))
}

func newCachePlugin(bp *handler.BP, args *Args) (*cachePlugin, error) {
	var c cache.DnsCache
	var err error
	if len(args.Redis) != 0 {
		c, err = cache.NewRedisCache(args.Redis)
		if err != nil {
			return nil, err
		}
	} else {
		if args.Size <= 0 {
			args.Size = 1024
		}

		maxSizePerShard := args.Size / 64
		if maxSizePerShard == 0 {
			maxSizePerShard = 1
		}

		if args.CleanerInterval == 0 {
			args.CleanerInterval = 120
		}

		c = cache.NewMemCache(64, maxSizePerShard, time.Duration(args.CleanerInterval)*time.Second)
	}
	return &cachePlugin{
		BP:   bp,
		args: args,
		c:    c,
	}, nil
}

// ExecES searches the cache. If cache hits, earlyStop will be true.
// It never returns an err, because a cache fault should not terminate the query process.
func (c *cachePlugin) ExecES(ctx context.Context, qCtx *handler.Context) (earlyStop bool, err error) {
	key, cacheHit := c.searchAndReply(ctx, qCtx)
	if cacheHit {
		return true, nil
	}

	if len(key) != 0 {
		de := newDeferStore(key, c)
		qCtx.DeferExec(de)
	}

	return false, nil
}

func (c *cachePlugin) searchAndReply(ctx context.Context, qCtx *handler.Context) (key string, cacheHit bool) {
	q := qCtx.Q()
	key, err := utils.GetMsgKey(q, 0)
	if err != nil {
		c.L().Warn("unable to get msg key, skip it", qCtx.InfoField(), zap.Error(err))
		return "", false
	}

	r, ttl, _, err := c.c.Get(ctx, key)
	if err != nil {
		c.L().Warn("unable to access cache, skip it", qCtx.InfoField(), zap.Error(err))
		return key, false
	}

	if r != nil { // if cache hit
		c.L().Debug("cache hit", qCtx.InfoField())
		r.Id = q.Id
		dnsutils.SetTTL(r, uint32(ttl/time.Second))
		qCtx.SetResponse(r, handler.ContextStatusResponded)
		return key, true
	}
	return key, false
}

type deferCacheStore struct {
	key string
	p   *cachePlugin
}

func newDeferStore(key string, p *cachePlugin) *deferCacheStore {
	return &deferCacheStore{key: key, p: p}
}

// Exec caches the response.
// It never returns an err, because a cache fault should not terminate the query process.
func (d *deferCacheStore) Exec(ctx context.Context, qCtx *handler.Context) (err error) {
	if err := d.exec(ctx, qCtx); err != nil {
		d.p.L().Warn("failed to cache the data", qCtx.InfoField(), zap.Error(err))
	}
	return nil
}

func (d *deferCacheStore) exec(ctx context.Context, qCtx *handler.Context) (err error) {
	r := qCtx.R()
	if r != nil && r.Rcode == dns.RcodeSuccess && r.Truncated == false && len(r.Answer) != 0 {
		ttl := dnsutils.GetMinimalTTL(r)
		if ttl > maxTTL {
			ttl = maxTTL
		}
		return d.p.c.Store(ctx, d.key, r, time.Duration(ttl)*time.Second)
	}
	return nil
}

func (c *cachePlugin) Connect(ctx context.Context, qCtx *handler.Context, pipeCtx *handler.PipeContext) (err error) {
	key, cacheHit := c.searchAndReply(ctx, qCtx)
	if cacheHit {
		return nil
	}

	err = pipeCtx.ExecNextPlugin(ctx, qCtx)
	if err != nil {
		return err
	}

	if len(key) != 0 {
		_ = newDeferStore(key, c).Exec(ctx, qCtx)
	}

	return nil
}

func preset(bp *handler.BP, args *Args) *cachePlugin {
	p, err := newCachePlugin(bp, args)
	if err != nil {
		panic(fmt.Sprintf("cache: preset plugin: %s", err))
	}
	return p
}
