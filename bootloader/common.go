// SPDX-FileCopyrightText: 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

package bootloader

import "regexp"

var RegRootUUID = regexp.MustCompile(`root=UUID=[0-9a-fA-F\-]+`)
