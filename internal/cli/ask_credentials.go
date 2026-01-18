// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func askCredentials() (string, string) {
	fd := int(os.Stdin.Fd())

	// 仅允许在交互式终端中输入凭据
	if !term.IsTerminal(fd) {
		printErrorAndExit(errors.New("this is not an interactive terminal, exiting"))
	}

	fmt.Print("Enter Username: ")

	reader := bufio.NewReader(os.Stdin)
	username, _ := reader.ReadString('\n')

	fmt.Print("Enter Password: ")

	// 读取密码时关闭回显，并在函数结束时恢复终端状态
	state, _ := term.GetState(fd)
	defer term.Restore(fd, state)
	bytePassword, _ := term.ReadPassword(fd)

	fmt.Print("\n")
	return strings.TrimSpace(username), strings.TrimSpace(string(bytePassword))
}
