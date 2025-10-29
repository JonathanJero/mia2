package commands

import (
    "backend/structs"
    "encoding/binary"
    "fmt"
    "os"
    "strings"
    "time"
)

// ExecuteMove - Mover archivo o carpeta a otro destino (cambio de referencias)
func ExecuteMove(path string, destino string) {
    // Validar parámetros obligatorios
    if path == "" {
        fmt.Println("Error: el parámetro -path es obligatorio.")
        return
    }

    if destino == "" {
        fmt.Println("Error: el parámetro -destino es obligatorio.")
        return
    }

    // Validar que hay una sesión activa
    session := GetCurrentSession()
    if session == nil {
        fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
        return
    }

    // Normalizar rutas
    path = strings.TrimSpace(path)
    if !strings.HasPrefix(path, "/") {
        path = "/" + path
    }

    destino = strings.TrimSpace(destino)
    if !strings.HasPrefix(destino, "/") {
        destino = "/" + destino
    }

    // Validar que no se intente mover la raíz
    if path == "/" {
        fmt.Println("Error: no se puede mover la raíz del sistema de archivos.")
        return
    }

    // Validar que destino no sea igual a origen
    if path == destino {
        fmt.Println("Error: el origen y el destino no pueden ser iguales.")
        return
    }

    // Validar que no se intente mover una carpeta dentro de sí misma
    if strings.HasPrefix(destino, path+"/") {
        fmt.Println("Error: no se puede mover una carpeta dentro de sí misma.")
        return
    }

    // Abrir el disco montado
    mounted := GetMountedPartition(session.PartitionID)
    if mounted == nil {
        fmt.Printf("Error: la partición '%s' no está montada.\n", session.PartitionID)
        return
    }

    file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
    if err != nil {
        fmt.Printf("Error al abrir el disco: %v\n", err)
        return
    }
    defer file.Close()

    // Obtener superbloque
    _, superblock, err := getPartitionAndSuperblock(file, mounted)
    if err != nil {
        fmt.Printf("Error al obtener superbloque: %v\n", err)
        return
    }

    // Parsear la ruta de origen
    parsedPath := parsePath(path)
    if parsedPath == nil {
        fmt.Printf("Error: ruta de origen inválida '%s'.\n", path)
        return
    }

    // Buscar el directorio padre del origen
    sourceParentInodeNum := int64(0) // Raíz por defecto

    // Navegar por los directorios del origen
    for _, dirName := range parsedPath.Directories {
        nextInode, err := findInodeInDirectory(file, superblock, sourceParentInodeNum, dirName)
        if err != nil {
            fmt.Printf("Error: no se encontró el directorio '%s': %v\n", dirName, err)
            return
        }
        sourceParentInodeNum = nextInode
    }

    // Buscar el archivo/carpeta de origen
    sourceInodeNum, err := findInodeInDirectory(file, superblock, sourceParentInodeNum, parsedPath.FileName)
    if err != nil {
        fmt.Printf("Error: no se encontró '%s': %v\n", parsedPath.FileName, err)
        return
    }

    // Leer el inodo del directorio padre del origen
    var sourceParentInode structs.Inodos
    sourceParentInodePos := superblock.S_inode_start + (sourceParentInodeNum * superblock.S_inode_s)
    file.Seek(sourceParentInodePos, 0)
    if err := binary.Read(file, binary.LittleEndian, &sourceParentInode); err != nil {
        fmt.Printf("Error al leer el inodo del directorio padre origen: %v\n", err)
        return
    }

    // Validar permisos de escritura sobre el directorio padre del origen
    if !checkWritePermissionOnInode(&sourceParentInode, session.User, session.Group) {
        fmt.Printf("Error: no tiene permisos de escritura sobre el directorio padre del origen.\n")
        return
    }

    // Leer el inodo de origen
    var sourceInode structs.Inodos
    sourceInodePos := superblock.S_inode_start + (sourceInodeNum * superblock.S_inode_s)
    file.Seek(sourceInodePos, 0)
    if err := binary.Read(file, binary.LittleEndian, &sourceInode); err != nil {
        fmt.Printf("Error al leer el inodo de origen: %v\n", err)
        return
    }

    // Validar permisos de escritura sobre el archivo/carpeta origen
    if !checkWritePermissionOnInode(&sourceInode, session.User, session.Group) {
        fmt.Printf("Error: no tiene permisos de escritura sobre '%s'.\n", path)
        return
    }

    // Parsear la ruta de destino
    parsedDestino := parsePath(destino)
    if parsedDestino == nil {
        fmt.Printf("Error: ruta de destino inválida '%s'.\n", destino)
        return
    }

    // Buscar el directorio de destino
    destInodeNum, err := findItemByPath(file, superblock, parsedDestino)
    if err != nil {
        fmt.Printf("Error: no se encontró el directorio de destino '%s': %v\n", destino, err)
        return
    }

    // Leer el inodo de destino
    var destInode structs.Inodos
    destInodePos := superblock.S_inode_start + (destInodeNum * superblock.S_inode_s)
    file.Seek(destInodePos, 0)
    if err := binary.Read(file, binary.LittleEndian, &destInode); err != nil {
        fmt.Printf("Error al leer el inodo de destino: %v\n", err)
        return
    }

    // Validar que el destino sea un directorio
    if destInode.I_type != '0' {
        fmt.Printf("Error: el destino '%s' no es un directorio.\n", destino)
        return
    }

    // Validar permisos de escritura sobre el destino
    if !checkWritePermissionOnInode(&destInode, session.User, session.Group) {
        fmt.Printf("Error: no tiene permisos de escritura sobre el directorio de destino '%s'.\n", destino)
        return
    }

    // Verificar que no exista ya un archivo/carpeta con el mismo nombre en el destino
    existingInode, _ := findInodeInDirectory(file, superblock, destInodeNum, parsedPath.FileName)
    if existingInode != -1 {
        fmt.Printf("Error: ya existe '%s' en el directorio de destino.\n", parsedPath.FileName)
        return
    }

    // Realizar el movimiento (cambio de referencias)
    fmt.Printf("📦 Moviendo '%s' a '%s'...\n", path, destino)

    // 1. Agregar la entrada en el directorio de destino
    if err := addEntryToDirectory(file, superblock, destInodeNum, parsedPath.FileName, sourceInodeNum); err != nil {
        fmt.Printf("Error al agregar la entrada en el directorio de destino: %v\n", err)
        return
    }

    // 2. Eliminar la entrada del directorio padre origen
    if err := removeEntryFromDirectoryMove(file, superblock, sourceParentInodeNum, parsedPath.FileName); err != nil {
        fmt.Printf("Error al eliminar la entrada del directorio origen: %v\n", err)
        return
    }

    // 3. Si es un directorio, actualizar la referencia del padre (..)
    if sourceInode.I_type == '0' {
        if err := updateParentReference(file, superblock, sourceInodeNum, destInodeNum); err != nil {
            fmt.Printf("Advertencia: no se pudo actualizar la referencia al padre: %v\n", err)
        }
    }

    // Actualizar el superbloque
    file.Seek(superblock.S_inode_start-int64(binary.Size(structs.SuperBloque{})), 0)
    if err := binary.Write(file, binary.LittleEndian, superblock); err != nil {
        fmt.Printf("Error al actualizar el superbloque: %v\n", err)
        return
    }

    // Determinar si es archivo o carpeta
    itemType := "archivo"
    if sourceInode.I_type == '0' {
        itemType = "carpeta"
    }

    fmt.Printf("✅ El %s '%s' fue movido exitosamente a '%s'.\n", itemType, parsedPath.FileName, destino)
}

// removeEntryFromDirectoryMove - Eliminar entrada de un directorio (para move)
func removeEntryFromDirectoryMove(file *os.File, superblock *structs.SuperBloque, parentInodeNum int64, entryName string) error {
    // Leer el inodo del directorio padre
    var parentInode structs.Inodos
    parentInodePos := superblock.S_inode_start + (parentInodeNum * superblock.S_inode_s)
    file.Seek(parentInodePos, 0)
    if err := binary.Read(file, binary.LittleEndian, &parentInode); err != nil {
        return err
    }

    // Buscar y eliminar la entrada en los bloques del directorio
    for i := 0; i < 15 && parentInode.I_block[i] != -1; i++ {
        blockPos := superblock.S_block_start + (parentInode.I_block[i] * superblock.S_block_s)
        file.Seek(blockPos, 0)

        var folderBlock structs.BloqueCarpeta
        if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
            continue
        }

        // Buscar la entrada
        for j := 0; j < 4; j++ {
            currentName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
            currentName = strings.TrimSpace(currentName)

            if currentName == entryName {
                // Limpiar la entrada
                folderBlock.BContent[j].BInodo = -1
                for k := range folderBlock.BContent[j].BName {
                    folderBlock.BContent[j].BName[k] = 0
                }

                // Escribir el bloque actualizado
                file.Seek(blockPos, 0)
                if err := binary.Write(file, binary.LittleEndian, &folderBlock); err != nil {
                    return err
                }

                // Actualizar el tiempo de modificación del directorio padre
                parentInode.I_mtime = time.Now().Unix()
                file.Seek(parentInodePos, 0)
                if err := binary.Write(file, binary.LittleEndian, &parentInode); err != nil {
                    return err
                }

                return nil
            }
        }
    }

    return fmt.Errorf("no se encontró la entrada '%s' en el directorio", entryName)
}

// updateParentReference - Actualizar la referencia .. de un directorio movido
func updateParentReference(file *os.File, superblock *structs.SuperBloque, dirInodeNum int64, newParentInodeNum int64) error {
    // Leer el inodo del directorio
    var dirInode structs.Inodos
    dirInodePos := superblock.S_inode_start + (dirInodeNum * superblock.S_inode_s)
    file.Seek(dirInodePos, 0)
    if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
        return err
    }

    // El primer bloque del directorio debería contener . y ..
    if dirInode.I_block[0] == -1 {
        return fmt.Errorf("el directorio no tiene bloques")
    }

    // Leer el primer bloque
    blockPos := superblock.S_block_start + (dirInode.I_block[0] * superblock.S_block_s)
    file.Seek(blockPos, 0)

    var folderBlock structs.BloqueCarpeta
    if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
        return err
    }

    // Buscar la entrada .. (debería estar en la posición 1)
    for i := 0; i < 4; i++ {
        entryName := strings.TrimRight(string(folderBlock.BContent[i].BName[:]), "\x00")
        entryName = strings.TrimSpace(entryName)

        if entryName == ".." {
            // Actualizar la referencia al nuevo padre
            folderBlock.BContent[i].BInodo = newParentInodeNum

            // Escribir el bloque actualizado
            file.Seek(blockPos, 0)
            if err := binary.Write(file, binary.LittleEndian, &folderBlock); err != nil {
                return err
            }

            return nil
        }
    }

    return fmt.Errorf("no se encontró la entrada '..' en el directorio")
}