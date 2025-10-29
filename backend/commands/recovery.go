package commands

import (
    "backend/structs"
    "encoding/binary"
    "fmt"
    "os"
    "time"
)

// ExecuteRecovery - Recuperar el sistema de archivos EXT3 desde el journaling
func ExecuteRecovery(id string) {
    // Validar parámetros obligatorios
    if id == "" {
        fmt.Println("Error: el parámetro -id es obligatorio.")
        return
    }

    // Buscar la partición montada
    mounted := GetMountedPartition(id)
    if mounted == nil {
        fmt.Printf("Error: la partición '%s' no está montada.\n", id)
        return
    }

    // Abrir el disco
    file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el disco: %v\n", err)
        return
    }
    defer file.Close()

    // Obtener la partición y superbloque
    partition, superblock, err := getPartitionAndSuperblock(file, mounted)
    if err != nil {
        fmt.Printf("Error al obtener superbloque: %v\n", err)
        return
    }

    // Verificar que sea sistema EXT3 (con journaling)
    if superblock.S_file_system_type != 3 {
        fmt.Println("Error: la partición no tiene un sistema de archivos EXT3 con journaling.")
        return
    }

    fmt.Printf("🔄 Iniciando recuperación del sistema de archivos EXT3 en '%s'...\n", id)
    fmt.Println()

    // Leer el journal de recuperación
	journalPos := partition.Part_start + int64(binary.Size(structs.SuperBloque{}))
	file.Seek(journalPos, 0)

	var journal structs.JournalRecovery
	if err := binary.Read(file, binary.LittleEndian, &journal); err != nil {
		fmt.Printf("Error al leer el journal: %v\n", err)
		return
	}

	// Verificar si hay información en el journal
	if journal.Journal_ultimo_montaje == 0 {
		fmt.Println("⚠️  No hay información de journaling disponible para recuperar.")
		return
	}

	fmt.Println("📋 Información del último estado consistente:")
	fmt.Printf("   Fecha: %s\n", time.Unix(journal.Journal_ultimo_montaje, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("   Inodos libres: %d\n", journal.Journal_inodos_libres)
	fmt.Printf("   Bloques libres: %d\n", journal.Journal_bloques_libres)
	fmt.Println()

    // Restaurar el superbloque desde el journal
    superblock.S_free_inodes_count = journal.Journal_inodos_libres
    superblock.S_free_blocks_count = journal.Journal_bloques_libres
    superblock.S_mtime = time.Now().Unix()
    superblock.S_mnt_count++

    // Restaurar bitmaps desde el journal
    fmt.Println("🔧 Restaurando bitmaps desde el journal...")

    // Restaurar bitmap de inodos
    file.Seek(superblock.S_bm_inode_start, 0)
    if err := binary.Write(file, binary.LittleEndian, &journal.Journal_bm_inodos); err != nil {
        fmt.Printf("Error al restaurar bitmap de inodos: %v\n", err)
        return
    }

    // Restaurar bitmap de bloques
    file.Seek(superblock.S_bm_block_start, 0)
    if err := binary.Write(file, binary.LittleEndian, &journal.Journal_bm_bloques); err != nil {
        fmt.Printf("Error al restaurar bitmap de bloques: %v\n", err)
        return
    }

    // Restaurar el área de inodos desde el journal
    fmt.Println("🔧 Restaurando área de inodos...")
    file.Seek(superblock.S_inode_start, 0)
    if err := binary.Write(file, binary.LittleEndian, &journal.Journal_inodos); err != nil {
        fmt.Printf("Error al restaurar área de inodos: %v\n", err)
        return
    }

    // Restaurar el área de bloques desde el journal
    fmt.Println("🔧 Restaurando área de bloques...")
    file.Seek(superblock.S_block_start, 0)
    if err := binary.Write(file, binary.LittleEndian, &journal.Journal_bloques); err != nil {
        fmt.Printf("Error al restaurar área de bloques: %v\n", err)
        return
    }

    // Escribir el superbloque actualizado
    file.Seek(partition.Part_start, 0)
    if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
        fmt.Printf("Error al actualizar el superbloque: %v\n", err)
        return
    }

    fmt.Println()
    fmt.Println("✅ Sistema de archivos recuperado exitosamente.")
    fmt.Printf("   📊 Inodos libres: %d\n", superblock.S_free_inodes_count)
    fmt.Printf("   📊 Bloques libres: %d\n", superblock.S_free_blocks_count)
    fmt.Println()
    fmt.Println("🎉 El sistema ha sido restaurado al último estado consistente.")
}

// ExecuteLoss - Simular pérdida del sistema de archivos
func ExecuteLoss(id string) {
    // Validar parámetros obligatorios
    if id == "" {
        fmt.Println("Error: el parámetro -id es obligatorio.")
        return
    }

    // Buscar la partición montada
    mounted := GetMountedPartition(id)
    if mounted == nil {
        fmt.Printf("Error: la partición '%s' no está montada.\n", id)
        return
    }

    // Abrir el disco
    file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el disco: %v\n", err)
        return
    }
    defer file.Close()

    // Obtener la partición y superbloque
    partition, superblock, err := getPartitionAndSuperblock(file, mounted)
    if err != nil {
        fmt.Printf("Error al obtener superbloque: %v\n", err)
        return
    }

    fmt.Printf("⚠️  ADVERTENCIA: Esta operación simulará una pérdida del sistema de archivos.\n")
    fmt.Printf("   Se limpiarán los siguientes bloques en '%s':\n", id)
    fmt.Println("   - Bitmap de Inodos")
    fmt.Println("   - Bitmap de Bloques")
    fmt.Println("   - Área de Inodos")
    fmt.Println("   - Área de Bloques")
    fmt.Println()

    // ✅ PASO CRÍTICO: Guardar el estado actual en el journal ANTES de limpiar
    fmt.Println("💾 Guardando estado actual en el journal de recuperación...")
    
    var journal structs.JournalRecovery
    journal.Journal_ultimo_montaje = time.Now().Unix()
    journal.Journal_inodos_libres = superblock.S_free_inodes_count
    journal.Journal_bloques_libres = superblock.S_free_blocks_count

    // Calcular tamaños de bitmaps
    bitmapInodosSize := superblock.S_inodes_count / 8
    if superblock.S_inodes_count%8 != 0 {
        bitmapInodosSize++
    }

    bitmapBloquesSize := superblock.S_blocks_count / 8
    if superblock.S_blocks_count%8 != 0 {
        bitmapBloquesSize++
    }

    // Leer bitmap de inodos actual
    file.Seek(superblock.S_bm_inode_start, 0)
    bitmapInodos := make([]byte, bitmapInodosSize)
    if _, err := file.Read(bitmapInodos); err != nil {
        fmt.Printf("Error al leer bitmap de inodos: %v\n", err)
        return
    }
    copy(journal.Journal_bm_inodos[:], bitmapInodos)

    // Leer bitmap de bloques actual
    file.Seek(superblock.S_bm_block_start, 0)
    bitmapBloques := make([]byte, bitmapBloquesSize)
    if _, err := file.Read(bitmapBloques); err != nil {
        fmt.Printf("Error al leer bitmap de bloques: %v\n", err)
        return
    }
    copy(journal.Journal_bm_bloques[:], bitmapBloques)

    // Leer área de inodos actual
    fmt.Println("   📦 Guardando área de inodos...")
    inodosAreaSize := superblock.S_inodes_count * superblock.S_inode_s
    file.Seek(superblock.S_inode_start, 0)
    inodosData := make([]byte, inodosAreaSize)
    if _, err := file.Read(inodosData); err != nil {
        fmt.Printf("Error al leer área de inodos: %v\n", err)
        return
    }
    copy(journal.Journal_inodos[:], inodosData)

    // Leer área de bloques actual
    fmt.Println("   📦 Guardando área de bloques...")
    bloquesAreaSize := superblock.S_blocks_count * superblock.S_block_s
    file.Seek(superblock.S_block_start, 0)
    bloquesData := make([]byte, bloquesAreaSize)
    if _, err := file.Read(bloquesData); err != nil {
        fmt.Printf("Error al leer área de bloques: %v\n", err)
        return
    }
    copy(journal.Journal_bloques[:], bloquesData)

    // ✅ ESCRIBIR EL JOURNAL AL DISCO (CRÍTICO!)
    journalPos := partition.Part_start + int64(binary.Size(structs.SuperBloque{}))
    file.Seek(journalPos, 0)
    if err := binary.Write(file, binary.LittleEndian, &journal); err != nil {
        fmt.Printf("Error al guardar el journal: %v\n", err)
        return
    }

    fmt.Println("   ✅ Estado guardado en el journal de recuperación")
    fmt.Printf("   📊 Inodos guardados: %d\n", superblock.S_inodes_count)
    fmt.Printf("   📊 Bloques guardados: %d\n", superblock.S_blocks_count)
    fmt.Println()

    // Crear buffer de ceros para limpieza
    zeroBuffer := make([]byte, 1024) // Buffer de 1KB

    // 1. Limpiar bitmap de inodos
    fmt.Println("🗑️  Limpiando bitmap de inodos...")
    
    file.Seek(superblock.S_bm_inode_start, 0)
    bytesWritten := int64(0)
    for bytesWritten < bitmapInodosSize {
        toWrite := int64(len(zeroBuffer))
        if bytesWritten+toWrite > bitmapInodosSize {
            toWrite = bitmapInodosSize - bytesWritten
        }
        if _, err := file.Write(zeroBuffer[:toWrite]); err != nil {
            fmt.Printf("Error al limpiar bitmap de inodos: %v\n", err)
            return
        }
        bytesWritten += toWrite
    }

    // 2. Limpiar bitmap de bloques
    fmt.Println("🗑️  Limpiando bitmap de bloques...")
    
    file.Seek(superblock.S_bm_block_start, 0)
    bytesWritten = 0
    for bytesWritten < bitmapBloquesSize {
        toWrite := int64(len(zeroBuffer))
        if bytesWritten+toWrite > bitmapBloquesSize {
            toWrite = bitmapBloquesSize - bytesWritten
        }
        if _, err := file.Write(zeroBuffer[:toWrite]); err != nil {
            fmt.Printf("Error al limpiar bitmap de bloques: %v\n", err)
            return
        }
        bytesWritten += toWrite
    }

    // 3. Limpiar área de inodos
    fmt.Println("🗑️  Limpiando área de inodos...")
    
    file.Seek(superblock.S_inode_start, 0)
    bytesWritten = 0
    for bytesWritten < inodosAreaSize {
        toWrite := int64(len(zeroBuffer))
        if bytesWritten+toWrite > inodosAreaSize {
            toWrite = inodosAreaSize - bytesWritten
        }
        if _, err := file.Write(zeroBuffer[:toWrite]); err != nil {
            fmt.Printf("Error al limpiar área de inodos: %v\n", err)
            return
        }
        bytesWritten += toWrite
    }

    // 4. Limpiar área de bloques
    fmt.Println("🗑️  Limpiando área de bloques...")
    
    file.Seek(superblock.S_block_start, 0)
    bytesWritten = 0
    for bytesWritten < bloquesAreaSize {
        toWrite := int64(len(zeroBuffer))
        if bytesWritten+toWrite > bloquesAreaSize {
            toWrite = bloquesAreaSize - bytesWritten
        }
        if _, err := file.Write(zeroBuffer[:toWrite]); err != nil {
            fmt.Printf("Error al limpiar área de bloques: %v\n", err)
            return
        }
        bytesWritten += toWrite
    }

    // ✅ NUEVO: Limpiar el journaling de operaciones
    fmt.Println("🧹 Limpiando entradas de journaling...")
    if err := ClearJournal(mounted); err != nil {
        fmt.Printf("⚠️  Advertencia al limpiar journaling: %v\n", err)
    }

    fmt.Println()
    fmt.Println("✅ Pérdida del sistema de archivos simulada exitosamente.")
    fmt.Println()
    fmt.Println("💡 Para recuperar el sistema de archivos, use:")
    fmt.Printf("   recovery -id=%s\n", id)
    fmt.Println()
    fmt.Println("📊 Recargue el explorador de archivos y el journaling para ver los cambios")
}