package app

import (
    "github.com/golang/geo/s2"
)


func parsePolyFile(filename []byte) int{
	if file, err := os.Open(filename); err == nil {
		defer file.Close()

        for scanner.Scan() {
            words := strings.Fields(scanner.Text())
            a, _ := strconv.ParseInt(words[0], 10, 64)
        }

		if scanErr := scanner.Err(); err != nil {
			log.Fatal(scanErr)
		}
	} else {
		log.Fatal(err)
	}
}



