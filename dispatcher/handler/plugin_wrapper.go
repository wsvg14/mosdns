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

package handler

import (
	"context"
	"fmt"
	"github.com/IrineSistiana/mosdns/dispatcher/mlog"
	"go.uber.org/zap"
)

var (
	_ ESExecutable     = (*PluginWrapper)(nil)
	_ Matcher          = (*PluginWrapper)(nil)
	_ ContextConnector = (*PluginWrapper)(nil)
	_ Service          = (*PluginWrapper)(nil)
)

// PluginWrapper wraps the original plugin to avoid extremely frequently
// interface conversion. To access the original plugin, use PluginWrapper.GetPlugin()
// Note: PluginWrapper not implements Executable.
// It automatically converts Executable to ESExecutable.
type PluginWrapper struct {
	p  Plugin
	e  Executable
	se ESExecutable
	m  Matcher
	cc ContextConnector
	s  Service
}

func NewPluginWrapper(gp Plugin) *PluginWrapper {
	w := new(PluginWrapper)
	w.p = gp

	if e, ok := gp.(Executable); ok {
		w.e = e
	}
	if se, ok := gp.(ESExecutable); ok {
		w.se = se
	}
	if m, ok := gp.(Matcher); ok {
		w.m = m
	}
	if cc, ok := gp.(ContextConnector); ok {
		w.cc = cc
	}
	if s, ok := gp.(Service); ok {
		w.s = s
	}

	return w
}

func (w *PluginWrapper) GetPlugin() Plugin {
	return w.p
}

func (w *PluginWrapper) logDebug(qCtx *Context) {
	mlog.L().Debug("exec plugin", qCtx.InfoField(), zap.String("exec", w.p.Tag()))
}

func (w *PluginWrapper) Connect(ctx context.Context, qCtx *Context, pipeCtx *PipeContext) (err error) {
	w.logDebug(qCtx)
	if err = ctx.Err(); err != nil {
		return err
	}

	if w.cc == nil {
		return fmt.Errorf("plugin tag: %s, type: %s is not a ContextConnector", w.p.Tag(), w.p.Type())
	}

	err = w.cc.Connect(ctx, qCtx, pipeCtx)
	if err != nil {
		return NewPluginError(w.p.Tag(), err)
	}
	return nil
}

func (w *PluginWrapper) Match(ctx context.Context, qCtx *Context) (matched bool, err error) {
	w.logDebug(qCtx)
	if err = ctx.Err(); err != nil {
		return false, err
	}

	if w.m == nil {
		return false, fmt.Errorf("plugin tag: %s, type: %s is not a Matcher", w.p.Tag(), w.p.Type())
	}

	matched, err = w.m.Match(ctx, qCtx)
	if err != nil {
		return false, NewPluginError(w.p.Tag(), err)
	}
	return matched, nil
}

func (w *PluginWrapper) ExecES(ctx context.Context, qCtx *Context) (earlyStop bool, err error) {
	w.logDebug(qCtx)
	if err = ctx.Err(); err != nil {
		return false, err
	}

	switch {
	case w.se != nil:
		earlyStop, err = w.se.ExecES(ctx, qCtx)
	case w.e != nil:
		err = w.e.Exec(ctx, qCtx)
	default:
		err = fmt.Errorf("plugin tag: %s, type: %s is not an ESExecutable nor Executable", w.p.Tag(), w.p.Type())
	}

	if err != nil {
		return false, NewPluginError(w.p.Tag(), err)
	}
	return earlyStop, nil
}

func (w *PluginWrapper) Shutdown() error {
	if w.s == nil {
		return fmt.Errorf("plugin tag: %s, type: %s is not a Service", w.p.Tag(), w.p.Type())
	}

	return w.s.Shutdown()
}

type PluginInterfaceType uint8

const (
	PITESExecutable = iota
	PITMatcher
	PITContextConnector
	PITService
)

func (w *PluginWrapper) Is(t PluginInterfaceType) bool {
	switch t {
	case PITESExecutable:
		return w.se != nil || w.e != nil
	case PITMatcher:
		return w.m != nil
	case PITContextConnector:
		return w.cc != nil
	case PITService:
		return w.s != nil
	default:
		panic(fmt.Sprintf("hander: invalid PluginInterfaceType: %d", t))
	}
}
