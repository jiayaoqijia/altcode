// Copied from github.com/altcode-ai/altcode/gateway/split.go
// Copyright (c) 2026 Ottie contributors — MIT License

package gateway

import (
	"strings"
)

// SplitMessage splits long messages into chunks, preserving code blocks.
// maxLen is measured in runes (Unicode characters), not bytes.
func SplitMessage(content string, maxLen int) []string {
	if maxLen <= 0 {
		if content == "" {
			return nil
		}
		return []string{content}
	}

	runes := []rune(content)
	totalLen := len(runes)
	var messages []string

	codeBlockBuffer := max(maxLen/10, 50)
	if codeBlockBuffer > maxLen/2 {
		codeBlockBuffer = maxLen / 2
	}

	start := 0
	for start < totalLen {
		remaining := totalLen - start
		if remaining <= maxLen {
			messages = append(messages, string(runes[start:totalLen]))
			break
		}

		effectiveLimit := max(maxLen-codeBlockBuffer, maxLen/2)
		end := start + effectiveLimit

		msgEnd := findLastNewline(runes, start, end, 200)
		if msgEnd <= start {
			msgEnd = findLastSpace(runes, start, end, 100)
		}
		if msgEnd <= start {
			msgEnd = end
		}

		unclosedIdx := findUnclosedCodeBlock(runes, start, msgEnd)

		if unclosedIdx >= 0 {
			if totalLen > msgEnd {
				closingIdx := findClosingCodeBlock(runes, msgEnd, totalLen)
				if closingIdx > 0 && closingIdx-start <= maxLen {
					msgEnd = closingIdx
				} else {
					headerEnd := findNewline(runes, unclosedIdx)
					var header string
					if headerEnd == -1 {
						header = strings.TrimSpace(
							string(runes[unclosedIdx : unclosedIdx+3]),
						)
					} else {
						header = strings.TrimSpace(
							string(runes[unclosedIdx:headerEnd]),
						)
					}
					headerEndIdx := unclosedIdx + len([]rune(header))
					if headerEnd != -1 {
						headerEndIdx = headerEnd
					}

					if msgEnd > headerEndIdx+20 {
						innerLimit := min(start+maxLen-5, totalLen)
						betterEnd := findLastNewline(
							runes, start, innerLimit, 200,
						)
						if betterEnd > headerEndIdx {
							msgEnd = betterEnd
						} else {
							msgEnd = innerLimit
						}
						chunk := strings.TrimRight(
							string(runes[start:msgEnd]), " \t\n\r",
						) + "\n```"
						messages = append(messages, chunk)
						rem := strings.TrimSpace(
							header + "\n" + string(runes[msgEnd:totalLen]),
						)
						runes = []rune(rem)
						totalLen = len(runes)
						start = 0
						continue
					}

					newEnd := findLastNewline(
						runes, start, unclosedIdx, 200,
					)
					if newEnd <= start {
						newEnd = findLastSpace(
							runes, start, unclosedIdx, 100,
						)
					}
					if newEnd > start {
						msgEnd = newEnd
					} else {
						if unclosedIdx-start > 20 {
							msgEnd = unclosedIdx
						} else {
							splitAt := min(start+maxLen-5, totalLen)
							chunk := strings.TrimRight(
								string(runes[start:splitAt]),
								" \t\n\r",
							) + "\n```"
							messages = append(messages, chunk)
							rem := strings.TrimSpace(
								header + "\n" +
									string(runes[splitAt:totalLen]),
							)
							runes = []rune(rem)
							totalLen = len(runes)
							start = 0
							continue
						}
					}
				}
			}
		}

		if msgEnd <= start {
			msgEnd = start + effectiveLimit
		}

		messages = append(messages, string(runes[start:msgEnd]))
		start = msgEnd
		for start < totalLen &&
			(runes[start] == ' ' || runes[start] == '\t' ||
				runes[start] == '\n' || runes[start] == '\r') {
			start++
		}
	}

	return messages
}

func findUnclosedCodeBlock(runes []rune, start, end int) int {
	inBlock := false
	lastOpen := -1
	for i := start; i < end; i++ {
		if i+2 < end &&
			runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			if !inBlock {
				lastOpen = i
			}
			inBlock = !inBlock
			i += 2
		}
	}
	if inBlock {
		return lastOpen
	}
	return -1
}

func findClosingCodeBlock(runes []rune, startIdx, end int) int {
	for i := startIdx; i < end; i++ {
		if i+2 < end &&
			runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			return i + 3
		}
	}
	return -1
}

func findNewline(runes []rune, from int) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == '\n' {
			return i
		}
	}
	return -1
}

func findLastNewline(runes []rune, start, end, window int) int {
	searchStart := max(end-window, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == '\n' {
			return i
		}
	}
	return start - 1
}

func findLastSpace(runes []rune, start, end, window int) int {
	searchStart := max(end-window, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i
		}
	}
	return start - 1
}
