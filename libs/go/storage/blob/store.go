// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package blob

import (
	"context"
	"io"
)

type Object struct {
	Attributes Attributes
	Body       io.ReadCloser
}

func (object *Object) Close() error {
	if object == nil || object.Body == nil {
		return nil
	}
	return object.Body.Close()
}

type Page struct {
	Objects    []Attributes
	NextCursor string
}

func (page Page) Clone() Page {
	clone := Page{NextCursor: page.NextCursor, Objects: make([]Attributes, len(page.Objects))}
	for index, attributes := range page.Objects {
		clone.Objects[index] = attributes.Clone()
	}
	return clone
}

type Store interface {
	Put(context.Context, Key, io.Reader, PutOptions) (Attributes, error)
	Open(context.Context, Key, GetOptions) (Object, error)
	Stat(context.Context, Key) (Attributes, error)
	Delete(context.Context, Key, DeleteOptions) error
	List(context.Context, ListOptions) (Page, error)
}
