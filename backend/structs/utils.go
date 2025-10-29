package structs

// BytesToString convierte un array de bytes a string eliminando padding
func BytesToString(b []byte) string {
    n := 0
    for n < len(b) && b[n] != 0 {
        n++
    }
    return string(b[:n])
}