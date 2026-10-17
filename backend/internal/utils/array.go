package utils

func Reduce[T, Y any](items []T, fn func(acc Y, item T) Y, init Y) Y {
	acc := init
	for _, item := range items {
		acc = fn(acc, item)
	}
	return acc
}

func Filter[T any](items []T, fn func(T) bool) []T {
	return Reduce(items, func(acc []T, item T) []T {
		if fn(item) {
			return append(acc, item)
		}
		return acc
	}, []T{})
}

func Map[T, Y any](items []T, fn func(T) (Y, error)) []Y {
	return Reduce(items, func(acc []Y, item T) []Y {
		y, err := fn(item)
		if err != nil {
			return nil
		}
		return append(acc, y)
	}, []Y{})
}
