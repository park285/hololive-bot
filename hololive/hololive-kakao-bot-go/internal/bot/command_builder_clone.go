package bot

func cloneCommandBuilders(src []CommandBuilder) []CommandBuilder {
	if src == nil {
		return nil
	}

	dst := make([]CommandBuilder, len(src))
	copy(dst, src)

	return dst
}
