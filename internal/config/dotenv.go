package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadDotEnv читает пары KEY=VALUE из файла и задаёт переменные окружения.
// Уже заданные переменные окружения не перезаписываются.
// Отсутствующий файл считается нормальной ситуацией (без ошибки).
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("не удалось открыть .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("ошибка разбора .env в %s:%d", path, lineNo)
		}

		key := strings.TrimSpace(parts[0])
		if key == "" {
			return fmt.Errorf("ошибка разбора .env в %s:%d (пустой ключ)", path, lineNo)
		}

		if existing, exists := os.LookupEnv(key); exists && existing != "" {
			continue
		}

		rawValue := strings.TrimSpace(parts[1])
		value, err := parseDotEnvValue(rawValue)
		if err != nil {
			return fmt.Errorf("ошибка разбора .env в %s:%d: %w", path, lineNo, err)
		}

		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("не удалось установить переменную %s из .env: %w", key, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ошибка чтения .env: %w", err)
	}
	return nil
}

func parseDotEnvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	if strings.HasPrefix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return unquoted, nil
	}

	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", fmt.Errorf("незакрытое значение в одинарных кавычках")
		}
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'"), nil
	}

	return value, nil
}
