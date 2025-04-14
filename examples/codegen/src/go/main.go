package main

import (
	proto "codegen/src/protobuf/pb"
	"fmt"
)

func main() {
	p := &proto.Person{
		Name:  "Grace Hopper",
		Id:    1,
		Email: "grace@hopper.com",
	}

	fmt.Printf("Name: %s, Id: %d, Email: %s\n", p.Name, p.Id, p.Email)
}
