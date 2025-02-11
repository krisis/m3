// This file is part of MinIO Kubernetes Cloud
// Copyright (c) 2019 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cluster

import (
	"context"
	"database/sql"
	"log"

	uuid "github.com/satori/go.uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// An application wide context that holds the a transaction, in case anything
// goes wrong during the business logic execution, database objects can be
// rolled back.
type Context struct {
	// tenant in question
	Tenant     *Tenant
	tenantTx   *sql.Tx
	tenantDB   *sql.DB
	mainTx     *sql.Tx
	ControlCtx context.Context
	// a user identifier of who is starting the context
	WhoAmI string
}

// MainTx returns a transaction against the Main DB, if none has been started, it starts one
func (c *Context) MainTx() (*sql.Tx, error) {
	if c.mainTx == nil {
		db := GetInstance().Db
		tx, err := db.BeginTx(c.ControlCtx, nil)
		if err != nil {
			return nil, err
		}
		c.mainTx = tx
	}
	return c.mainTx, nil
}

// TenantDB returns a configured DB connection for the Tenant DB
func (c *Context) TenantDB() *sql.DB {
	if c.tenantDB == nil {
		db := GetInstance().GetTenantDB(c.Tenant.ShortName)
		c.tenantDB = db
	}
	return c.tenantDB
}

// TenantTx returns a transaction against the Tenant DB, if none has been started, it starts one
func (c *Context) TenantTx() (*sql.Tx, error) {
	if c.tenantTx == nil {
		db := c.TenantDB()
		tx, err := db.BeginTx(c.ControlCtx, nil)
		if err != nil {
			return nil, err
		}
		c.tenantTx = tx
	}
	return c.tenantTx, nil
}

// Commit commits the any transaction that was started on this context
func (c *Context) Commit() error {
	// commit tenant schema tx
	if c.tenantTx != nil {
		err := c.tenantTx.Commit()
		if err != nil {
			return err
		}
		// restart the txn
		c.tenantTx = nil
	}
	// commit main schema tx
	if c.mainTx != nil {
		err := c.mainTx.Commit()
		if err != nil {
			return err
		}
		// restart the txn
		c.mainTx = nil
	}
	return nil
}

func (c *Context) Rollback() error {
	// rollback tenant schema tx
	if c.tenantTx != nil {
		err := c.tenantTx.Rollback()
		if err != nil {
			return err
		}
		// restart the txn
		c.tenantTx = nil
	}
	// rollback main schema tx
	if c.mainTx != nil {
		err := c.mainTx.Rollback()
		if err != nil {
			return err
		}
		// restart the txn
		c.mainTx = nil
	}
	return nil
}

// Creates a new `Context` with no tenant tenant that holds transaction and `context.Context`
// to control timeouts and cancellations.
func NewEmptyContext() (*Context, error) {
	return NewCtxWithTenant(nil), nil
}

// Creates a new `Context` with no tenant tenant that holds transaction and `context.Context`
// to control timeouts and cancellations starting from a grpc context which should contain wether the user
// is authenticated or not
func NewEmptyContextWithGrpcContext(ctx context.Context) (*Context, error) {
	appCtx := NewCtxWithTenant(nil)
	var whoAmI string
	if ctx.Value(WhoAmIKey) != nil {
		whoAmI = ctx.Value(WhoAmIKey).(string)
	}
	if whoAmI != "" {
		appCtx.WhoAmI = whoAmI
	}
	appCtx.ControlCtx = ctx
	return appCtx, nil
}

func NewCtxWithTenant(tenant *Tenant) *Context {
	// we are going to default the control context to background
	ctlCtx := context.Background()
	c := &Context{Tenant: tenant, ControlCtx: ctlCtx}
	return c
}

// Creates a new `Context` with no tenant tenant that holds transaction and `context.Context`
// to control timeouts and cancellations starting from a grpc context which should contain wether the user
// is authenticated or not
func NewTenantContextWithGrpcContext(ctx context.Context) (*Context, error) {

	// get tenant ID from context
	tenantIDStr := ctx.Value(TenantIDKey).(string)
	tenantID, _ := uuid.FromString(tenantIDStr)
	// get the tenant record
	tenant, err := GetTenantByID(&tenantID)
	if err != nil {
		log.Println(err)
		return nil, status.New(codes.Internal, "internal error").Err()
	}
	// create a context with the tenant
	appCtx := NewCtxWithTenant(&tenant)
	var whoAmI string
	if ctx.Value(WhoAmIKey) != nil {
		whoAmI = ctx.Value(WhoAmIKey).(string)
	}
	if whoAmI != "" {
		appCtx.WhoAmI = whoAmI
	}
	appCtx.ControlCtx = ctx
	return appCtx, nil
}
