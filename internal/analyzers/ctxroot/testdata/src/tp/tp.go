package tp

import "context"

func Work() {
	context.Background() // want "context.Background or context.TODO outside allow_paths"
}
