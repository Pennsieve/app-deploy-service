package dydbutils

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func StringAttributeValue(attributeValue string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: attributeValue}
}

func FromItem[T any](item map[string]types.AttributeValue) (*T, error) {
	if len(item) == 0 {
		return nil, nil
	}
	var value T
	if err := attributevalue.UnmarshalMap(item, &value); err != nil {
		return nil, fmt.Errorf("error unmarshalling item to %T: %w", value, err)
	}
	return &value, nil
}

func ItemImpl[T any](value T) (map[string]types.AttributeValue, error) {
	item, err := attributevalue.MarshalMap(value)
	if err != nil {
		return nil, fmt.Errorf("error marshalling %+v to item: %w", value, err)
	}
	return item, nil
}
