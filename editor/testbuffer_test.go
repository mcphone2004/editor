package editor_test

import "github.com/anthonybrice/editor/buffer/fake"

func newFakeBuffer(content string) *fake.Buffer {
	return fake.New(content)
}
