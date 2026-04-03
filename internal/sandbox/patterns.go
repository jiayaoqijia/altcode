package sandbox

// defaultSafeBlockList returns patterns blocked in PolicySafe mode.
func defaultSafeBlockList() []string {
	return []string{
		"rm -rf /",
		"rm -rf ~",
		"rm -rf .",
		"dd if=",
		"mkfs",
		"fdisk",
		":(){ :|:& };:",
		"> /dev/sd",
		"chmod -R 777 /",
		"curl | sh",
		"curl | bash",
		"wget | sh",
		"wget | bash",
		"| sh",
		"| bash",
	}
}

// defaultReadOnlyBlockList blocks write operations.
func defaultReadOnlyBlockList() []string {
	list := defaultSafeBlockList()
	list = append(list,
		"rm ",
		"mv ",
		"cp ",
		"mkdir ",
		"rmdir ",
		"touch ",
		"chmod ",
		"chown ",
		"tee ",
		"git push",
		"git commit",
		"git reset",
	)
	return list
}
