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

package api

import (
	"context"

	uuid "github.com/satori/go.uuid"

	"github.com/lib/pq"
	pb "github.com/minio/m3/api/stubs"
	"github.com/minio/m3/cluster"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	uniqueViolationError = "unique_violation"
	defaultRequestLimit  = 25
)

func (s *server) UserWhoAmI(ctx context.Context, in *pb.Empty) (*pb.User, error) {
	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, err
	}
	// get User ID from context
	userIDStr := ctx.Value(cluster.UserIDKey).(string)
	userID, _ := uuid.FromString(userIDStr)
	// Get user row from db
	userObj, err := cluster.GetUserByID(appCtx, userID)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	return &pb.User{
		Name:  userObj.Name,
		Email: userObj.Email,
		Id:    userObj.ID.String()}, nil
}

// UserAddInvite invites a new user to the tenant's system by sending an email
func (s *server) UserAddInvite(ctx context.Context, in *pb.InviteRequest) (*pb.Empty, error) {

	reqName := in.GetName()
	reqEmail := in.GetEmail()

	newUser := cluster.User{Name: reqName, Email: reqEmail}

	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			appCtx.Rollback()
			return
		}
		// if no error happened to this point commit transaction
		err = appCtx.Commit()
	}()

	// Create user on db
	err = cluster.AddUser(appCtx, &newUser)
	if err != nil {
		_, ok := err.(*pq.Error)
		if ok {
			if err.(*pq.Error).Code.Name() == uniqueViolationError {
				return nil, status.New(codes.InvalidArgument, "Email and/or Name already exist").Err()
			}
		}
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	// Send email invitation with token
	err = cluster.InviteUserByEmail(appCtx, cluster.TokenSignupEmail, &newUser)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	return &pb.Empty{}, err
}

// UserResetPasswordInvite invites a new user to reset their password by sending them an email
func (s *server) UserResetPasswordInvite(ctx context.Context, in *pb.InviteRequest) (*pb.Empty, error) {
	reqEmail := in.GetEmail()

	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			appCtx.Rollback()
			return
		}
		// if no error happened to this point commit transaction
		err = appCtx.Commit()
	}()

	user, err := cluster.GetUserByEmail(appCtx, reqEmail)
	if err != nil {
		return nil, status.New(codes.Internal, "User Not Found").Err()
	}

	// Send email invitation with token
	err = cluster.InviteUserByEmail(appCtx, cluster.TokenResetPasswordEmail, &user)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	return &pb.Empty{}, err
}

func (s *server) AddUser(ctx context.Context, in *pb.AddUserRequest) (*pb.User, error) {
	reqName := in.GetName()
	reqEmail := in.GetEmail()
	newUser := cluster.User{Name: reqName, Email: reqEmail}

	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, err
	}

	err = cluster.AddUser(appCtx, &newUser)
	if err != nil {
		appCtx.Rollback()
		_, ok := err.(*pq.Error)
		if ok {
			if err.(*pq.Error).Code.Name() == uniqueViolationError {
				return nil, status.New(codes.InvalidArgument, "Email and/or Name already exist").Err()
			}
		}
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	err = appCtx.Commit()
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	return &pb.User{Name: newUser.Name, Email: newUser.Email}, nil
}

func (s *server) ListUsers(ctx context.Context, in *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	reqOffset := in.GetOffset()
	reqLimit := in.GetLimit()
	if reqLimit == 0 {
		reqLimit = defaultRequestLimit
	}
	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, err
	}
	// Get list of users set maximum 25 per page
	users, err := cluster.GetUsersForTenant(appCtx, reqOffset, reqLimit)
	if err != nil {
		return nil, status.New(codes.Internal, "Error getting Users").Err()
	}

	var respUsers []*pb.User
	for _, user := range users {
		// TODO create a WhoAmI endpoint instead of using IsMe on ListUsers
		usr := &pb.User{
			Id:      user.ID.String(),
			Name:    user.Name,
			Email:   user.Email,
			Enabled: user.Enabled}
		respUsers = append(respUsers, usr)

	}
	return &pb.ListUsersResponse{Users: respUsers, TotalUsers: int32(len(respUsers))}, nil
}

// ChangePassword Gets the old password, validates it and sets new password to the user.
func (s *server) ChangePassword(ctx context.Context, in *pb.ChangePasswordRequest) (res *pb.Empty, err error) {
	newPassword := in.GetNewPassword()
	if newPassword == "" {
		return nil, status.New(codes.InvalidArgument, "Empty New Password").Err()
	}
	oldPassword := in.GetOldPassword()
	if oldPassword == "" {
		return nil, status.New(codes.InvalidArgument, "Empty Old Password").Err()
	}

	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	defer func() {
		if err != nil {
			appCtx.Rollback()
			return
		}
		// if no error happened to this point commit transaction
		err = appCtx.Commit()
	}()
	// get User ID from context
	userIDStr := ctx.Value(cluster.UserIDKey).(string)
	userID, _ := uuid.FromString(userIDStr)
	// Get user row from db
	userObj, err := cluster.GetUserByID(appCtx, userID)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	// Comparing the old password with the hash stored password
	if err = bcrypt.CompareHashAndPassword([]byte(userObj.Password), []byte(in.OldPassword)); err != nil {
		return nil, status.New(codes.Unauthenticated, "Wrong credentials").Err()
	}
	// Hash the new password and update the it
	err = cluster.SetUserPassword(appCtx, &userObj.ID, newPassword)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	// get session ID from context
	sessionRowID := ctx.Value(cluster.SessionIDKey).(string)
	// Invalidate Session
	err = cluster.UpdateSessionStatus(appCtx, sessionRowID, "invalid")
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	return &pb.Empty{}, err
}

func (s *server) DisableUser(ctx context.Context, in *pb.UserActionRequest) (*pb.UserActionResponse, error) {
	reqUserID := in.GetId()
	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}
	err = cluster.SetUserEnabled(appCtx, reqUserID, false)
	if err != nil {
		appCtx.Rollback()
		return nil, status.New(codes.Internal, "Error disabling user").Err()
	}
	appCtx.Commit()
	return &pb.UserActionResponse{Status: "false"}, nil
}

func (s *server) EnableUser(ctx context.Context, in *pb.UserActionRequest) (*pb.UserActionResponse, error) {
	reqUserID := in.GetId()
	appCtx, err := cluster.NewTenantContextWithGrpcContext(ctx)
	if err != nil {
		return nil, status.New(codes.Internal, err.Error()).Err()
	}

	err = cluster.SetUserEnabled(appCtx, reqUserID, true)
	if err != nil {
		return nil, status.New(codes.Internal, "Error enabling user").Err()
	}
	return &pb.UserActionResponse{Status: "true"}, nil
}
