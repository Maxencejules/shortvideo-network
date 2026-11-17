package main

import (
	"fmt"
	"log"
	"os"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/social"
)

func main() {
	log.SetFlags(0)
	base, token := "", ""
	for _, arg := range os.Args[1:] {
		switch {
		case len(arg) > 6 && arg[:6] == "-base=":
			base = arg[6:]
		case len(arg) > 7 && arg[:7] == "-token=":
			token = arg[7:]
		default:
			log.Fatal("usage: svn-feed-client -base=http://127.0.0.1:port -token=TOKEN")
		}
	}
	ids, err := social.FollowingIDs(base, token)
	if err != nil {
		log.Fatal(err)
	}
	for _, id := range ids {
		fmt.Println(id)
	}
}
