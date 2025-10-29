package commands

import (
	"backend/structs"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"
)

// WriteJournal escribe una entrada al journaling
func WriteJournal(mounted *MountedPartition, operation, path, content string) error {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return err
	}

	// Calcular posición inicial del journaling y cantidad de entradas.
	journalSize := int64(binary.Size(structs.Journal{}))
	journalingCount := 64
	if superblock.S_file_system_type == 3 {
		journalingCount = 50
	}

	journalStart := superblock.S_bm_inode_start - (int64(journalingCount) * journalSize)

	// Buscar la primera entrada libre (JCount == -1) — mkfs inicializa a -1
	var freeIndex int64 = -1
	for i := 0; i < journalingCount; i++ {
		journalPosition := journalStart + int64(i)*journalSize
		file.Seek(journalPosition, 0)

		var journal structs.Journal
		if err := binary.Read(file, binary.LittleEndian, &journal); err != nil {
			continue
		}

		if journal.JCount == -1 {
			freeIndex = int64(i)
			break
		}
	}

	if freeIndex == -1 {
		// Si el journal está lleno, sobrescribir la entrada más antigua (índice 0)
		freeIndex = 0
		fmt.Println("⚠️ Journal lleno, sobrescribiendo entrada más antigua")
	}

	// Crear nueva entrada de journal
	var newJournal structs.Journal
	newJournal.JCount = int32(freeIndex + 1) // JCount como contador de la operación

	// Llenar Information
	copy(newJournal.JContent.IOperation[:], []byte(operation))
	// ❌ REMOVIDO: newJournal.JContent.IType = fileType (este campo no existe)
	copy(newJournal.JContent.IPath[:], []byte(path))

	// Limitar el contenido a 100 bytes
	if len(content) > 100 {
		copy(newJournal.JContent.IContent[:], []byte(content[:100]))
	} else {
		copy(newJournal.JContent.IContent[:], []byte(content))
	}

	newJournal.JContent.IDate = float32(time.Now().Unix())

	// Escribir la entrada al journal
	journalPosition := journalStart + (freeIndex * int64(binary.Size(structs.Journal{})))
	file.Seek(journalPosition, 0)

	if err := binary.Write(file, binary.LittleEndian, &newJournal); err != nil {
		return fmt.Errorf("error al escribir journal: %v", err)
	}

	fmt.Printf("✅ Journal escrito: %s en %s (índice %d)\n", operation, path, freeIndex)
	return nil
}

// RepairJournal scans for possible journal entries written with different
// layouts and consolidates valid entries into the preferred journal region.
// It creates a backup of the original journal region before rewriting.
func RepairJournal(mounted *MountedPartition) (int, error) {
	if mounted == nil {
		return 0, errors.New("mounted partition is nil")
	}
	f, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return 0, fmt.Errorf("open partition file: %w", err)
	}
	defer f.Close()

	// prepare sizes
	journalSize := int64(binary.Size(structs.Journal{}))
	preferredCount := int64(50)

	// read superblock from partition start
	sb, err := readSuperBlockMixed(f, mounted.Start)
	if err != nil {
		return 0, fmt.Errorf("leer superbloque: %w", err)
	}

	// candidate starts: older code used a different start (using S_inode_start)
	cand1Start := sb.S_inode_start - (64 * journalSize) // older layout (64 entries)
	cand1Count := int64(64)

	// preferred start/layout (current): S_bm_inode_start - journalingCount*journalSize
	prefStart := sb.S_bm_inode_start - (preferredCount * journalSize)
	prefCount := preferredCount

	now := float32(time.Now().Unix())

	type foundEntry struct {
		idx  int64
		when float32
		j    structs.Journal
	}
	found := make([]foundEntry, 0)

	// sanitizer helper (local): trim zeros and convert bytes to printable string
	sanitizeBytesLocal := func(b []byte) string {
		// trim trailing zeros
		i := len(b)
		for i > 0 && b[i-1] == 0 {
			i--
		}
		b = b[:i]
		if len(b) == 0 {
			return ""
		}
		// decode runes safely, replace invalid/ non-printable with '?'
		var out []rune
		for len(b) > 0 {
			r, size := utf8.DecodeRune(b)
			if r == utf8.RuneError && size == 1 {
				out = append(out, '?')
				b = b[1:]
				continue
			}
			if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
				out = append(out, '?')
			} else {
				out = append(out, r)
			}
			b = b[size:]
		}
		return string(out)
	}

	// helper to scan region (more permissive)
	scanRegion := func(start int64, count int64) error {
		for i := int64(0); i < count; i++ {
			off := start + i*journalSize
			if off < 0 {
				continue
			}
			if _, err := f.Seek(off, 0); err != nil {
				continue
			}
			var j structs.Journal
			if err := binary.Read(f, binary.LittleEndian, &j); err != nil {
				continue
			}

			opBytes := bytesTrimZero(j.JContent.IOperation[:])
			pthBytes := bytesTrimZero(j.JContent.IPath[:])
			contentBytes := bytesTrimZero(j.JContent.IContent[:])

			op := sanitizeBytesLocal(opBytes)
			pth := sanitizeBytesLocal(pthBytes)
			_ = sanitizeBytesLocal(contentBytes) // not used for key but useful to sanitize later

			when := j.JContent.IDate
			// tolerar timestamps inválidos -> usar now
			if when <= 0 || when != when {
				when = now
			}

			// accept if at least operation or path is non-empty
			if op == "" && pth == "" {
				continue
			}

			found = append(found, foundEntry{idx: i, when: when, j: j})
		}
		return nil
	}

	// scan both candidate regions
	_ = scanRegion(cand1Start, cand1Count)
	_ = scanRegion(prefStart, prefCount)

	if len(found) == 0 {
		return 0, errors.New("no valid journal entries found in candidate regions")
	}

	// deduplicate by timestamp+operation+path
	uniqMap := map[string]struct{}{}
	uniq := make([]structs.Journal, 0, len(found))
	for _, fe := range found {
		key := fmt.Sprintf("%v|%s|%s", fe.when, string(bytesTrimZero(fe.j.JContent.IOperation[:])), string(bytesTrimZero(fe.j.JContent.IPath[:])))
		if _, ok := uniqMap[key]; ok {
			continue
		}
		uniqMap[key] = struct{}{}
		uniq = append(uniq, fe.j)
	}

	// sort by timestamp
	sort.Slice(uniq, func(i, j int) bool {
		return uniq[i].JContent.IDate < uniq[j].JContent.IDate
	})

	// backup current preferred region
	backupName := fmt.Sprintf("/tmp/journal_backup_%s_%d.bin", mounted.ID, time.Now().Unix())
	if _, err := f.Seek(prefStart, 0); err == nil {
		buf := make([]byte, prefCount*journalSize)
		if _, err := f.ReadAt(buf, prefStart); err == nil {
			_ = os.WriteFile(backupName, buf, 0644)
		}
	}

	// prepare empty journals (all slots set to free: JCount = -1)
	emptyJ := structs.Journal{}
	emptyJ.JCount = -1

	// write normalized region
	if _, err := f.Seek(prefStart, 0); err != nil {
		return 0, fmt.Errorf("seek prefStart: %w", err)
	}
	// clear region
	for i := int64(0); i < prefCount; i++ {
		if err := binary.Write(f, binary.LittleEndian, &emptyJ); err != nil {
			return 0, fmt.Errorf("clear region write: %w", err)
		}
	}

	// write recovered entries into beginning of region
	for i, j := range uniq {
		j.JCount = int32(i + 1)
		// enforce we use current time if timestamp is weird (shouldn't happen now)
		if j.JContent.IDate < 1000000000 || j.JContent.IDate != j.JContent.IDate {
			j.JContent.IDate = float32(time.Now().Unix())
		}
		off := prefStart + int64(i)*journalSize
		if _, err := f.Seek(off, 0); err != nil {
			continue
		}
		_ = binary.Write(f, binary.LittleEndian, &j)
	}

	return len(uniq), nil
}

// helper: trim trailing zero bytes
func bytesTrimZero(b []byte) []byte {
	i := len(b)
	for i > 0 && b[i-1] == 0 {
		i--
	}
	return b[:i]
}

// helper: printable UTF-8 and not empty
func isPrintableUTF8(s string) bool {
	if s == "" {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// ClearJournal limpia todas las entradas del journaling (comando loss)
func ClearJournal(mounted *MountedPartition) error {
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Obtener partición y superbloque
	_, superblock, err := getPartitionAndSuperblock(file, mounted)
	if err != nil {
		return err
	}

	// Calcular posición inicial del journaling
	journalStart := superblock.S_inode_start - int64(binary.Size(structs.Journal{})*64)

	// Limpiar todas las entradas
	var emptyJournal structs.Journal
	for i := 0; i < 64; i++ {
		journalPosition := journalStart + int64(i*binary.Size(structs.Journal{}))
		file.Seek(journalPosition, 0)

		if err := binary.Write(file, binary.LittleEndian, &emptyJournal); err != nil {
			return fmt.Errorf("error al limpiar journal %d: %v", i, err)
		}
	}

	fmt.Println("✅ Journaling limpiado completamente")
	return nil
}

// DumpJournalRegions devuelve los bytes raw de las regiones candidatas del journal
// en formato bruto para diagnóstico. Devuelve un mapa con claves "preferred" y "candidate1".
func DumpJournalRegions(mounted *MountedPartition) (map[string][]byte, error) {
	if mounted == nil {
		return nil, fmt.Errorf("mounted partition is nil")
	}
	f, err := os.OpenFile(mounted.Path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open partition file: %w", err)
	}
	defer f.Close()

	journalSize := int64(binary.Size(structs.Journal{}))
	preferredCount := int64(50)

	sb, err := readSuperBlockMixed(f, mounted.Start)
	if err != nil {
		return nil, fmt.Errorf("leer superbloque: %w", err)
	}

	cand1Start := sb.S_inode_start - (64 * journalSize)
	cand1Count := int64(64)
	prefStart := sb.S_bm_inode_start - (preferredCount * journalSize)
	prefCount := preferredCount

	out := make(map[string][]byte)

	if cand1Start > 0 {
		buf := make([]byte, cand1Count*journalSize)
		if _, err := f.ReadAt(buf, cand1Start); err == nil {
			out["candidate1"] = buf
		} else {
			out["candidate1"] = []byte{}
		}
	} else {
		out["candidate1"] = []byte{}
	}

	if prefStart > 0 {
		buf := make([]byte, prefCount*journalSize)
		if _, err := f.ReadAt(buf, prefStart); err == nil {
			out["preferred"] = buf
		} else {
			out["preferred"] = []byte{}
		}
	} else {
		out["preferred"] = []byte{}
	}

	return out, nil
}
