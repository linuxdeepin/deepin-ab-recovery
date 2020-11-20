package pmoncfg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePmonCfgFile(t *testing.T) {
	type args struct {
		filename string
	}
	tests := []struct {
		name    string
		args    args
		want    *PmonCfg
		wantErr bool
	}{
		{
			name: "ParsePmonCfgFile",
			args: args{
				filename: "testdata/boot.cfg",
			},
			want: &PmonCfg{
				defaultItem: 0,
				timeout:     3,
				showMenu:    0,
				items: []*menuEntry{
					{
						title:  "UnionTech OS Desktop 20 Pro GNU/Linux 4.19.0-loongson-3-desktop",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
					{
						title:  "Roll back to xxxxx # ab-recovery",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "ParsePmonCfgFile_Bad",
			args: args{
				filename: "testdata/boot_bad.cfg",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePmonCfgFile(tt.args.filename)
			if tt.wantErr {
				assert.NotNil(t, err)
				return
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPmonCfg_RemoveRecoveryMenuEntries(t *testing.T) {
	type fields struct {
		defaultItem int
		timeout     int
		showMenu    int
		items       []*menuEntry
	}
	tests := []struct {
		name  string
		cfg   *PmonCfg
		after *PmonCfg
	}{
		{
			name: "RemoveRecoveryMenuEntries",
			cfg: &PmonCfg{
				defaultItem: 0,
				timeout:     3,
				showMenu:    0,
				items: []*menuEntry{
					{
						title:  "UnionTech OS Desktop 20 Pro GNU/Linux 4.19.0-loongson-3-desktop",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
					{
						title:  "Roll back to xxxxx # ab-recovery",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
				},
			},
			after: &PmonCfg{
				defaultItem: 0,
				timeout:     3,
				showMenu:    0,
				items: []*menuEntry{
					{
						title:  "UnionTech OS Desktop 20 Pro GNU/Linux 4.19.0-loongson-3-desktop",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.RemoveRecoveryMenuEntries()
			assert.Equal(t, tt.after, tt.cfg)
		})
	}
}

func TestPmonCfg_AddRecoveryMenuEntry(t *testing.T) {
	type args struct {
		menuText string
		rootUUID string
		linux    string
		initrd   string
	}
	tests := []struct {
		name  string
		cfg   *PmonCfg
		args  args
		after *PmonCfg
	}{
		{
			name: "AddRecoveryMenuEntry",
			cfg: &PmonCfg{
				defaultItem: 0,
				timeout:     3,
				showMenu:    0,
				items: []*menuEntry{
					{
						title:  "UnionTech OS Desktop 20 Pro GNU/Linux 4.19.0-loongson-3-desktop",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
				},
			},
			args: args{
				menuText: "testtitle",
				rootUUID: "a13e2b9d-572f-4a25-ab8f-b2eda8c3f8ea",
				linux:    "/vmlinuz",
				initrd:   "/initrd.img",
			},
			after: &PmonCfg{
				defaultItem: 0,
				timeout:     3,
				showMenu:    0,
				items: []*menuEntry{
					{
						title:  "UnionTech OS Desktop 20 Pro GNU/Linux 4.19.0-loongson-3-desktop",
						kernel: "/dev/fs/ext2@wd0/vmlinuz-4.19.0-loongson-3-desktop",
						initrd: "/dev/fs/ext2@wd0/initrd.img-4.19.0-loongson-3-desktop",
						args:   "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19",
					},
					{
						title:  "testtitle" + recoveryTitleSuffix,
						kernel: "/dev/fs/ext2@wd0/vmlinuz",
						initrd: "/dev/fs/ext2@wd0/initrd.img",
						args:   "root=UUID=a13e2b9d-572f-4a25-ab8f-b2eda8c3f8ea console=tty loglevel=0 quiet splash",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.AddRecoveryMenuEntry(tt.args.menuText, tt.args.rootUUID, tt.args.linux, tt.args.initrd)
			assert.Equal(t, tt.after, tt.cfg)
		})
	}
}

func TestPmonCfg_ReplaceRootUuid(t *testing.T) {
	filename := "testdata/boot.cfg"

	cfg, err := ParsePmonCfgFile(filename)
	assert.Nil(t, err)

	cfg.ReplaceRootUuid("a13e2b9d-572f-4a25-ab8f-b2eda8c3f8ea")

	assert.Equal(t, "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=a13e2b9d-572f-4a25-ab8f-b2eda8c3f8ea", cfg.items[0].args)
	assert.Equal(t, "console=tty loglevel=0 locales=zh_CN.UTF-8  splash quiet console=tty loglevel=0 root=UUID=14cbf2c4-9982-4f9e-be1e-71a2b3d35e19", cfg.items[1].args)
}
