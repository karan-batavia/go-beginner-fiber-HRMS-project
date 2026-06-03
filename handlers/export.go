package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// EmployeePayrollRow is one row of the consolidated export — joins
// employee identity, payroll amounts, bank account, and tax IDs into a
// single record for the accountant's month-end workbook.
type EmployeePayrollRow struct {
	EmployeeID      string  `bson:"_id"`
	Name            string  `bson:"name"`
	Email           string  `bson:"email"`
	NationalID      string  `bson:"national_id"`
	TaxID           string  `bson:"tax_id"`
	Department      string  `bson:"department"`
	Designation     string  `bson:"designation"`
	BankName        string  `bson:"bank_name"`
	AccountNumber   string  `bson:"account_number"`
	RoutingNumber   string  `bson:"routing_number"`
	GrossPay        float64 `bson:"gross_pay"`
	TaxWithholding  float64 `bson:"tax_withholding"`
	NetPay          float64 `bson:"net_pay"`
	PayPeriod       string  `bson:"pay_period"`
}

// ExportPayroll emits the consolidated CSV. The Mongo aggregation joins
// the employees, payroll_runs, employee_bank_accounts, and tax_ids
// collections by employee_id and projects the columns the accountant
// wants in one stream.
func ExportPayroll(db *mongo.Database) fiber.Handler {
	return func(c *fiber.Ctx) error {
		period := c.Query("period")

		filter := bson.M{}
		if period != "" {
			filter["pay_period"] = period
		}

		pipeline := []bson.M{
			{"$match": filter},
			{"$lookup": bson.M{
				"from":         "employees",
				"localField":   "employee_id",
				"foreignField": "_id",
				"as":           "employee",
			}},
			{"$lookup": bson.M{
				"from":         "employee_bank_accounts",
				"localField":   "employee_id",
				"foreignField": "employee_id",
				"as":           "bank",
			}},
			{"$lookup": bson.M{
				"from":         "tax_ids",
				"localField":   "employee_id",
				"foreignField": "employee_id",
				"as":           "tax",
			}},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cur, err := db.Collection("payroll_runs").Aggregate(ctx, pipeline)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		defer cur.Close(ctx)

		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", fmt.Sprintf(
			`attachment; filename="payroll-%s.csv"`, period,
		))
		w := csv.NewWriter(c.Response().BodyWriter())
		w.Write([]string{
			"pay_period", "employee_id", "name", "email",
			"national_id", "tax_id", "department", "designation",
			"bank_name", "account_number", "routing_number",
			"gross_pay", "tax_withholding", "net_pay",
		})

		for cur.Next(ctx) {
			var doc bson.M
			if err := cur.Decode(&doc); err != nil {
				continue
			}
			emp := firstDoc(doc, "employee")
			bank := firstDoc(doc, "bank")
			tax := firstDoc(doc, "tax")
			w.Write([]string{
				asString(doc["pay_period"]),
				asString(doc["employee_id"]),
				asString(emp["name"]),
				asString(emp["email"]),
				asString(emp["national_id"]),
				asString(tax["tax_id"]),
				asString(emp["department"]),
				asString(emp["designation"]),
				asString(bank["bank_name"]),
				asString(bank["account_number"]),
				asString(bank["routing_number"]),
				strconv.FormatFloat(asFloat(doc["gross_pay"]), 'f', 2, 64),
				strconv.FormatFloat(asFloat(doc["tax_withholding"]), 'f', 2, 64),
				strconv.FormatFloat(asFloat(doc["net_pay"]), 'f', 2, 64),
			})
		}
		w.Flush()
		return nil
	}
}

func firstDoc(parent bson.M, key string) bson.M {
	if arr, ok := parent[key].(bson.A); ok && len(arr) > 0 {
		if m, ok := arr[0].(bson.M); ok {
			return m
		}
	}
	return bson.M{}
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
