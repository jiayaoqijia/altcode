package lsp

import "os"

// initializeParams builds the params for the initialize request.
func initializeParams(rootPath string) map[string]any {
	return map[string]any{
		"processId": os.Getpid(),
		"clientInfo": map[string]string{
			"name":    "altcode",
			"version": "0.1.0",
		},
		"rootUri": "file://" + rootPath,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"synchronization": map[string]any{
					"didSave": true,
				},
			},
		},
		"workspaceFolders": []map[string]string{
			{"uri": "file://" + rootPath, "name": rootPath},
		},
	}
}

// didOpenParams builds the textDocument/didOpen notification params.
func didOpenParams(uri, languageID, text string) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    1,
			"text":       text,
		},
	}
}

// didChangeParams builds a textDocument/didChange notification with
// full document replacement.
func didChangeParams(uri, text string) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"uri":     uri,
			"version": 2,
		},
		"contentChanges": []map[string]any{
			{"text": text},
		},
	}
}

// positionParams builds a TextDocumentPositionParams used by
// definition, hover, etc.
func positionParams(uri string, line, char int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"uri": uri,
		},
		"position": map[string]int{
			"line":      line,
			"character": char,
		},
	}
}
