// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"errors"
	"fmt"

	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/validator"
)

func resetPassword(store *storage.Storage) {
	// 交互式读取用户名和新密码
	username, password := askCredentials()
	user, err := store.UserByUsername(username)
	if err != nil {
		printErrorAndExit(err)
	}

	if user == nil {
		printErrorAndExit(errors.New("user not found"))
	}

	userModificationRequest := &model.UserModificationRequest{
		Password: &password,
	}
	// 校验密码规则并更新用户记录
	if validationErr := validator.ValidateUserModification(store, user.ID, userModificationRequest); validationErr != nil {
		printErrorAndExit(validationErr.Error())
	}

	user.Password = password
	if err := store.UpdateUser(user); err != nil {
		printErrorAndExit(err)
	}

	fmt.Println("Password changed!")
}
