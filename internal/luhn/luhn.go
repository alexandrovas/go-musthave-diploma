package luhn

// IsValid проверяет номер заказа по алгоритму Луна.
// Возвращает false, если строка пустая или содержит нецифровые символы
func IsValid(number string) bool {
	if len(number) == 0 {
		return false
	}

	sum := 0
	double := false

	// Проходим справа налево, каждую вторую цифру умножаем на 2
	for i := len(number) - 1; i >= 0; i-- {
		digit := int(number[i] - '0')
		if digit < 0 || digit > 9 {
			return false
		}

		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}

		sum += digit
		double = !double
	}

	return sum%10 == 0
}
