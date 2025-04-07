package error

func GetUnwrappedListOrEmpty(err error) []error {
	if err == nil {
		return []error{}
	}
	if unwrappedList, ok := err.(interface{ Unwrap() []error }); ok {
		// we are dealing with a wrapped error list
		return unwrappedList.Unwrap()
	}
	return []error{}
}
