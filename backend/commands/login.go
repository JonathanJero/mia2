package commands

import (
	"backend/structs"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ExecuteLogin(user, pass, id string) {
	// Verificar que no haya sesión activa **real**.
	// Si hay una sesión temporal creada por el modo script (AllowCommandsWithoutSession)
	// y dicha sesión es la automática '__auto__', permitimos que el comando login
	// sobrescriba la sesión para que el script pueda dejar una sesión persistente.
	if IsSessionActive() {
		// Obtener la sesión actual para determinar su origen
		if sess := GetCurrentSession(); sess != nil {
			// Si la sesión actual no es la sesión automática de scripting, rechazamos
			if !(AllowCommandsWithoutSession && sess.User == "__auto__") {
				fmt.Println("Error: Ya existe una sesión activa. Use 'logout' para cerrar la sesión actual.")
				return
			}
			// Si es la sesión automática, permitimos continuar y que StartSession la reemplace.
		} else {
			// Por seguridad, si IsSessionActive() devolvió true pero no podemos obtener la sesión,
			// rechazamos el login.
			fmt.Println("Error: Ya existe una sesión activa. Use 'logout' para cerrar la sesión actual.")
			return
		}
	}

	// Validar parámetros obligatorios
	if user == "" {
		fmt.Println("Error: el parámetro -user es obligatorio para login.")
		return
	}

	if pass == "" {
		fmt.Println("Error: el parámetro -pass es obligatorio para login.")
		return
	}

	if id == "" {
		fmt.Println("Error: el parámetro -id es obligatorio para login.")
		return
	}

	// Buscar la partición montada por ID
	mounted := GetMountedPartition(id)
	if mounted == nil {
		fmt.Printf("Error: No se encontró ninguna partición montada con ID '%s'.\n", id)
		return
	}

	// Leer el archivo users.txt del sistema de archivos
	usersContent, err := readUsersFile(mounted)
	if err != nil {
		fmt.Printf("Error al leer archivo users.txt: %v\n", err)
		return
	}

	// Buscar el usuario en users.txt y obtener toda su información
	userInfo, found := findUserWithInfo(usersContent, user, pass)
	if !found {
		fmt.Printf("Error: Usuario '%s' no encontrado o contraseña incorrecta.\n", user)
		return
	}

	// Determinar si es usuario root (UID=1 y nombre="root")
	isRoot := (userInfo.UID == 1 && user == "root")

	// Iniciar sesión con toda la información
	StartSession(user, userInfo.GroupName, id, userInfo.UID, userInfo.GID, isRoot)

	fmt.Println("✅ Sesión iniciada exitosamente.")
	fmt.Printf("   👤 Usuario: %s\n", user)
	fmt.Printf("   👥 Grupo: %s\n", userInfo.GroupName)
	fmt.Printf("   🆔 UID: %d | GID: %d\n", userInfo.UID, userInfo.GID)
	fmt.Printf("   📀 Partición: %s\n", id)

	if isRoot {
		fmt.Println("   🔑 Rol: Administrador (root)")
		fmt.Println("   ⚡ Acceso completo a todos los discos y particiones")
	} else {
		fmt.Println("   🔑 Rol: Usuario estándar")
		fmt.Printf("   📂 Acceso limitado a la partición: %s\n", id)
	}
}

// Leer el archivo users.txt del sistema de archivos
func readUsersFile(mounted *MountedPartition) (string, error) {
	// Abrir el archivo del disco
	file, err := os.OpenFile(mounted.Path, os.O_RDWR, 0644)
	if err != nil {
		return "", fmt.Errorf("error al abrir el disco: %v", err)
	}
	defer file.Close()

	// Leer el MBR para obtener información de la partición
	var mbr structs.MBR
	if err := binary.Read(file, binary.LittleEndian, &mbr); err != nil {
		return "", fmt.Errorf("error al leer el MBR: %v", err)
	}

	// Encontrar la partición específica
	var partition *structs.Partition
	for _, p := range mbr.Mbr_partitions {
		if p.Part_status != '0' {
			partitionNameBytes := p.Part_name[:]
			nullIndex := len(partitionNameBytes)
			for j, b := range partitionNameBytes {
				if b == 0 {
					nullIndex = j
					break
				}
			}
			partitionName := strings.TrimSpace(string(partitionNameBytes[:nullIndex]))

			if strings.EqualFold(partitionName, mounted.Name) || partitionName == mounted.Name {
				partitionCopy := p
				partition = &partitionCopy
				break
			}
		}
	}

	if partition == nil {
		return "", fmt.Errorf("no se pudo encontrar la partición '%s'", mounted.Name)
	}

	// Leer el superbloque
	file.Seek(partition.Part_start, 0)
	var superblock structs.SuperBloque
	if err := binary.Read(file, binary.LittleEndian, &superblock); err != nil {
		return "", fmt.Errorf("error al leer el superbloque: %v", err)
	}

	// Leer el inodo de users.txt (inodo 1)
	inodePosition := superblock.S_inode_start + (1 * superblock.S_inode_s)

	file.Seek(inodePosition, 0)
	var usersInode structs.Inodos
	if err := binary.Read(file, binary.LittleEndian, &usersInode); err != nil {
		return "", fmt.Errorf("error al leer el inodo de users.txt: %v", err)
	}

	// Leer múltiples bloques si es necesario
	var allContent []byte

	for i := 0; i < 15; i++ { // Máximo 15 bloques directos
		if usersInode.I_block[i] == -1 {
			break // No hay más bloques
		}

		blockPosition := superblock.S_block_start + (usersInode.I_block[i] * superblock.S_block_s)
		file.Seek(blockPosition, 0)

		var block structs.BloqueArchivo
		if err := binary.Read(file, binary.LittleEndian, &block); err != nil {
			return "", fmt.Errorf("error al leer bloque %d: %v", i, err)
		}

		allContent = append(allContent, block.BContent[:]...)
	}

	// Usar el tamaño del inodo para obtener el contenido exacto
	var content string
	if usersInode.I_s > 0 && usersInode.I_s <= int64(len(allContent)) {
		content = string(allContent[:usersInode.I_s])
	} else {
		content = string(allContent)
	}

	return content, nil
}

// findUserWithInfo - Buscar usuario y retornar toda su información
func findUserWithInfo(usersContent, user, pass string) (UserInfo, bool) {
	lines := strings.Split(usersContent, "\n")

	// Primero, encontrar todos los grupos para hacer el mapeo
	groups := make(map[string]string)         // GID -> Nombre del grupo
	groupsByName := make(map[string]string)   // Nombre -> GID
	groupGIDNumeric := make(map[string]int64) // GID string -> GID numérico

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) >= 3 {
			gid := strings.TrimSpace(parts[0])
			tipo := strings.TrimSpace(parts[1])
			groupName := strings.TrimSpace(parts[2])

			if tipo == "G" && gid != "0" { // Es un grupo y no está eliminado
				groups[gid] = groupName       // GID numérico -> nombre
				groupsByName[groupName] = gid // nombre -> GID numérico

				// Convertir GID a numérico
				if gidNum, err := strconv.ParseInt(gid, 10, 64); err == nil {
					groupGIDNumeric[gid] = gidNum
				}
			}
		}
	}

	// Ahora buscar el usuario
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) >= 5 {
			uidStr := strings.TrimSpace(parts[0])
			tipo := strings.TrimSpace(parts[1])
			gidStr := strings.TrimSpace(parts[2]) // Este puede ser número o nombre
			username := strings.TrimSpace(parts[3])
			password := strings.TrimSpace(parts[4])

			if tipo == "U" && uidStr != "0" { // Es un usuario y no está eliminado
				if username == user && password == pass {
					// Convertir UID a numérico
					uid, err := strconv.ParseInt(uidStr, 10, 64)
					if err != nil {
						continue
					}

					// Buscar el nombre del grupo y GID numérico
					var groupName string
					var gidNumeric int64

					// Caso 1: gidStr es numérico (buscar por GID)
					if foundName, exists := groups[gidStr]; exists {
						groupName = foundName
						if gidNum, exists := groupGIDNumeric[gidStr]; exists {
							gidNumeric = gidNum
						} else {
							gidNumeric, _ = strconv.ParseInt(gidStr, 10, 64)
						}
					} else {
						// Caso 2: gidStr es el nombre del grupo directamente
						if foundGID, exists := groupsByName[gidStr]; exists {
							groupName = gidStr
							if gidNum, exists := groupGIDNumeric[foundGID]; exists {
								gidNumeric = gidNum
							} else {
								gidNumeric, _ = strconv.ParseInt(foundGID, 10, 64)
							}
						} else {
							groupName = "unknown"
							gidNumeric = 0
						}
					}

					userInfo := UserInfo{
						UID:       uid,
						GID:       gidNumeric,
						GroupName: groupName,
						Username:  username,
					}

					return userInfo, true
				}
			}
		}
	}

	return UserInfo{}, false
}

// Comando LOGOUT
func ExecuteLogout() {
	if !IsSessionActive() {
		fmt.Println("Error: No hay sesión activa para cerrar.")
		return
	}

	session := GetCurrentSession()
	fmt.Printf("Cerrando sesión del usuario '%s'.\n", session.User)
	EndSession()
	fmt.Println("✅ Sesión cerrada exitosamente.")
}
