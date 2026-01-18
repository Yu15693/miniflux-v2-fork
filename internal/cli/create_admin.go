// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"log/slog"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/validator"
)

func createAdminUserFromEnvironmentVariables(store *storage.Storage) {
	// 从环境变量读取管理员账号信息
	createAdminUser(store, config.Opts.AdminUsername(), config.Opts.AdminPassword())
}

func createAdminUserFromInteractiveTerminal(store *storage.Storage) {
	// 交互式输入管理员账号信息
	username, password := askCredentials()
	createAdminUser(store, username, password)
}

func createAdminUser(store *storage.Storage, username, password string) {
	// 构造管理员创建请求
	userCreationRequest := &model.UserCreationRequest{
		Username: username,
		Password: password,
		IsAdmin:  true,
	}

	// 已存在则跳过
	if store.UserExists(userCreationRequest.Username) {
		slog.Info("Skipping admin user creation because it already exists",
			slog.String("username", userCreationRequest.Username),
		)
		return
	}

	// 校验用户输入（用户名/密码规则）
	if validationErr := validator.ValidateUserCreationWithPassword(store, userCreationRequest); validationErr != nil {
		printErrorAndExit(validationErr.Error())
	}

	// 写入数据库并打印创建结果
	if user, err := store.CreateUser(userCreationRequest); err != nil {
		printErrorAndExit(err)
	} else {
		slog.Info("Created new admin user",
			slog.String("username", user.Username),
			slog.Int64("user_id", user.ID),
		)
	}
}
