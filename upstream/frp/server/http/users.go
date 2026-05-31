package http

import (
	"fmt"
	stdhttp "net/http"

	"github.com/fatedier/frp/pkg/util/http"
	"github.com/fatedier/frp/pkg/util/jsonx"
	"github.com/fatedier/frp/server/userstore"
)

func (c *Controller) ListUsers(ctx *http.Context) (any, error) {
	if c.userStore == nil {
		return nil, http.NewError(stdhttp.StatusNotFound, "user store is disabled")
	}
	if err := c.requireAdmin(ctx); err != nil {
		return nil, err
	}
	return c.userStore.List(), nil
}

func (c *Controller) GetUser(ctx *http.Context) (any, error) {
	if c.userStore == nil {
		return nil, http.NewError(stdhttp.StatusNotFound, "user store is disabled")
	}
	if err := c.requireAdmin(ctx); err != nil {
		return nil, err
	}
	name := ctx.Param("name")
	u, ok := c.userStore.Get(name)
	if !ok {
		return nil, http.NewError(stdhttp.StatusNotFound, fmt.Sprintf("user %q not found", name))
	}
	return u, nil
}

func (c *Controller) UpsertUser(ctx *http.Context) (any, error) {
	if c.userStore == nil {
		return nil, http.NewError(stdhttp.StatusNotFound, "user store is disabled")
	}
	if err := c.requireAdmin(ctx); err != nil {
		return nil, err
	}
	body, err := ctx.Body()
	if err != nil {
		return nil, http.NewError(stdhttp.StatusBadRequest, err.Error())
	}
	var payload userstore.User
	if err := jsonx.Unmarshal(body, &payload); err != nil {
		return nil, http.NewError(stdhttp.StatusBadRequest, err.Error())
	}
	if name := ctx.Param("name"); name != "" {
		payload.Name = name
	}
	u, err := c.userStore.Upsert(payload)
	if err != nil {
		return nil, http.NewError(stdhttp.StatusBadRequest, err.Error())
	}
	return u, nil
}

func (c *Controller) DeleteUser(ctx *http.Context) (any, error) {
	if c.userStore == nil {
		return nil, http.NewError(stdhttp.StatusNotFound, "user store is disabled")
	}
	if err := c.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := c.userStore.Delete(ctx.Param("name")); err != nil {
		return nil, http.NewError(stdhttp.StatusBadRequest, err.Error())
	}
	return http.GeneralResponse{Code: 200, Msg: "success"}, nil
}
