package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ExecuteFind - Buscar archivos o carpetas por patrón de nombre
func ExecuteFind(path string, name string) {
	// Validar parámetros obligatorios
	if path == "" {
		fmt.Println("Error: el parámetro -path es obligatorio.")
		return
	}

	if name == "" {
		fmt.Println("Error: el parámetro -name es obligatorio.")
		return
	}

	// Validar que hay una sesión activa
	session := GetCurrentSession()
	if session == nil {
		fmt.Println("Error: no hay ninguna sesión activa. Use el comando 'login' primero.")
		return
	}

	// Normalizar la ruta
	path = strings.TrimSpace(path)
	// Quitar comillas envolventes si el script las pasó (ej. "/")
	path = strings.Trim(path, "\"'")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Normalizar el nombre de búsqueda y quitar comillas envolventes
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "\"'")

	// Abrir el disco montado
	mounted := GetMountedPartition(session.PartitionID)
	if mounted == nil {
		fmt.Printf("Error: la partición '%s' no está montada.\n", session.PartitionID)
		return
	}

	file, err := os.OpenFile(mounted.Path, os.O_RDONLY, 0644)
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

	// Determinar el inodo de inicio
	var startInodeNum int64

	if path == "/" {
		startInodeNum = 0
	} else {
		parsedPath := parsePath(path)
		if parsedPath == nil {
			fmt.Printf("Error: ruta inválida '%s'.\n", path)
			return
		}

		startInodeNum, err = findItemByPath(file, superblock, parsedPath)
		if err != nil {
			fmt.Printf("Error: no se encontró la ruta '%s': %v\n", path, err)
			return
		}
	}

	// Leer el inodo de inicio
	var startInode structs.Inodos
	startInodePos := superblock.S_inode_start + (startInodeNum * superblock.S_inode_s)
	file.Seek(startInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &startInode); err != nil {
		fmt.Printf("Error al leer el inodo de inicio: %v\n", err)
		return
	}

	// Validar que la ruta de inicio sea un directorio
	if startInode.I_type != '0' {
		fmt.Printf("Error: la ruta '%s' no es un directorio.\n", path)
		return
	}

	// Convertir el patrón a expresión regular
	pattern := convertPatternToRegex(name)
	regex, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Printf("Error al compilar el patrón de búsqueda: %v\n", err)
		return
	}

	// Realizar la búsqueda
	fmt.Printf("🔍 Buscando archivos/carpetas que coincidan con '%s' en '%s':\n\n", name, path)

	results := make([]FindResult, 0)
	searchRecursive(file, superblock, startInodeNum, path, regex, session.User, session.Group, &results)

	// Mostrar resultados
	if len(results) == 0 {
		fmt.Println("No se encontraron archivos o carpetas que coincidan con el patrón.")
		return
	}

	// Imprimir árbol de resultados
	printFindTree(results, path)
	fmt.Printf("\n✅ Se encontraron %d resultados.\n", len(results))
}

// FindResult - Estructura para almacenar resultados de búsqueda
type FindResult struct {
	Path       string
	Name       string
	IsDir      bool
	Level      int
	ParentPath string
}

// searchRecursive - Buscar recursivamente en directorios
func searchRecursive(file *os.File, superblock *structs.SuperBloque, dirInodeNum int64, currentPath string, pattern *regexp.Regexp, username string, groupname string, results *[]FindResult) {
	var dirInode structs.Inodos
	dirInodePos := superblock.S_inode_start + (dirInodeNum * superblock.S_inode_s)
	file.Seek(dirInodePos, 0)
	if err := binary.Read(file, binary.LittleEndian, &dirInode); err != nil {
		return
	}

	if !checkReadPermissionOnInode(&dirInode, username, groupname) {
		return
	}

	for i := 0; i < 15 && dirInode.I_block[i] != -1; i++ {
		blockPos := superblock.S_block_start + (dirInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPos, 0)

		var folderBlock structs.BloqueCarpeta
		if err := binary.Read(file, binary.LittleEndian, &folderBlock); err != nil {
			continue
		}

		for j := 0; j < 4; j++ {
			entryName := strings.TrimRight(string(folderBlock.BContent[j].BName[:]), "\x00")
			entryName = strings.TrimSpace(entryName)

			if entryName == "" || entryName == "." || entryName == ".." {
				continue
			}

			entryInodeNum := folderBlock.BContent[j].BInodo
			if entryInodeNum == -1 {
				continue
			}

			var entryInode structs.Inodos
			entryInodePos := superblock.S_inode_start + (entryInodeNum * superblock.S_inode_s)
			file.Seek(entryInodePos, 0)
			if err := binary.Read(file, binary.LittleEndian, &entryInode); err != nil {
				continue
			}

			if !checkReadPermissionOnInode(&entryInode, username, groupname) {
				continue
			}

			var fullPath string
			if currentPath == "/" {
				fullPath = "/" + entryName
			} else {
				fullPath = currentPath + "/" + entryName
			}

			if pattern.MatchString(entryName) {
				result := FindResult{
					Path:       fullPath,
					Name:       entryName,
					IsDir:      entryInode.I_type == '0',
					Level:      countSlashes(fullPath),
					ParentPath: currentPath,
				}
				*results = append(*results, result)
			}

			if entryInode.I_type == '0' {
				searchRecursive(file, superblock, entryInodeNum, fullPath, pattern, username, groupname, results)
			}
		}
	}
}

// convertPatternToRegex - Convertir patrón con * y ? a expresión regular
func convertPatternToRegex(pattern string) string {
	result := regexp.QuoteMeta(pattern)
	result = strings.ReplaceAll(result, "\\*", ".*")
	result = strings.ReplaceAll(result, "\\?", ".")
	result = "^" + result + "$"
	return result
}

// countSlashes - Contar el número de / en una ruta para determinar el nivel
func countSlashes(path string) int {
	if path == "/" {
		return 0
	}
	return strings.Count(path, "/")
}

// printFindTree - Imprimir los resultados en forma de árbol
func printFindTree(results []FindResult, basePath string) {
	if len(results) == 0 {
		return
	}

	fmt.Println(basePath)

	// Organizar resultados por ruta completa
	allPaths := make(map[string]FindResult)
	for _, result := range results {
		allPaths[result.Path] = result
	}

	// Agregar directorios intermedios
	for _, result := range results {
		addIntermediateDirs(result.Path, basePath, allPaths)
	}

	// Organizar por padre
	resultsByParent := make(map[string][]FindResult)
	for _, result := range allPaths {
		resultsByParent[result.ParentPath] = append(resultsByParent[result.ParentPath], result)
	}

	// Imprimir recursivamente
	printTreeLevelFinal(basePath, resultsByParent, 0)
}

// addIntermediateDirs - Agregar directorios intermedios
func addIntermediateDirs(path string, _ string, allPaths map[string]FindResult) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	currentPath := ""

	for i, part := range parts {
		if part == "" {
			continue
		}

		var fullPath string
		switch currentPath {
		case "":
			fullPath = "/" + part
			currentPath = "/"
		case "/":
			fullPath = "/" + part
		default:
			fullPath = currentPath + "/" + part
		}

		if fullPath == path {
			break
		}

		if _, exists := allPaths[fullPath]; !exists {
			parentPath := currentPath
			if parentPath == "" {
				parentPath = "/"
			}

			allPaths[fullPath] = FindResult{
				Path:       fullPath,
				Name:       part,
				IsDir:      true,
				Level:      i + 1,
				ParentPath: parentPath,
			}
		}

		if currentPath == "/" {
			currentPath = "/" + part
		} else {
			currentPath = currentPath + "/" + part
		}
	}
}

// printTreeLevelFinal - Imprimir un nivel del árbol
func printTreeLevelFinal(currentPath string, resultsByParent map[string][]FindResult, level int) {
	items, exists := resultsByParent[currentPath]

	if !exists || len(items) == 0 {
		return
	}

	for _, item := range items {
		indent := strings.Repeat("   ", level)
		itemType := ""
		if item.IsDir {
			itemType = " (carpeta)"
		}

		fmt.Printf("%s|_ %s%s\n", indent, item.Name, itemType)

		if item.IsDir {
			printTreeLevelFinal(item.Path, resultsByParent, level+1)
		}
	}
}
