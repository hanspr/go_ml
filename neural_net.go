package ml

import (
	"fmt"
	"log"
	"github.com/alonsovidales/go_matrix"
	"io/ioutil"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// NeuralNet Neural network representation, the X and Y properties are to be
// used with training proposals
type NeuralNet struct {
	// Training set of values for each feature, the first dimension are the test cases
	X [][]float32
	cudaXtransBias *mt.CudaMatrix
	// The training set with values to be predicted
	Y [][]float32
	cudaYtrans *mt.CudaMatrix
	// 1st dim -> layer, 2nd dim -> neuron, 3rd dim theta
	Theta [][][]float32

	zBuff []*mt.CudaMatrix
	aBuff []*mt.CudaMatrix
	dBuff []*mt.CudaMatrix
	gradRemoveBiasBuff []*mt.CudaMatrix
	gradBuff []*mt.CudaMatrix
	copyBuffTheta []*mt.CudaMatrix
	biasTopThetaBuff []*mt.CudaMatrix
	copyBuff []*mt.CudaMatrix
	removeBiasThetaBuff []*mt.CudaMatrix
	thetaTransBuff []*mt.CudaMatrix
	dBuffMult []*mt.CudaMatrix
	subJBuff *mt.CudaMatrix
	cudaMultAllElemsBuff []*mt.CudaMatrix
	buffInitted bool
}

func (nn *NeuralNet) InitFmincg() {
	mt.StartBufferingMem("nncost")
	mt.FreeMem()

	fmt.Println("X")
	nn.cudaYtrans = mt.GetCudaMatrix(nn.Y).Trans()
	nn.cudaXtransBias = mt.GetCudaMatrix(nn.X).Trans().AddBiasTop()
	fmt.Println("END X")

	mt.SetDefaultBuff()
}

// CostFunction Calcualtes the cost function for the training set stored in the
// X and Y properties of the instance, and with the theta configuration.
// The lambda parameter controls the degree of regularization (0 means
// no-regularization, infinity means ignoring all input variables because all
// coefficients of them will be zero)
// The calcGrad param in case of true calculates the gradient in addition of the
// cost, and in case of false, only calculates the cost
func (nn *NeuralNet) CostFunction(lambda float32, calcGrad bool) (j float32, grad [][][]float32, err error) {
	//log.Println("-------- INIT COST --------")
	if len(nn.Y) == 0 || len(nn.X) == 0 || len(nn.Theta) == 0 {
		err = fmt.Errorf("the lenght of the X, Y or Theta params are zero")
		return
	}

	if len(nn.Y) != len(nn.X) {
		err = fmt.Errorf(
			"the length of the X parameter doesn't corresponds with the length of the Y parameter")
		return
	}

	if len(nn.Theta[len(nn.Theta)-1]) != len(nn.Y[0]) {
		err = fmt.Errorf(
			"the length of the last theta layer should to correspond with the length of the expected results")
		return
	}

	mt.FreeMem()
	mt.StartBufferingMem("nncost")
	defer mt.SetDefaultBuff()

	//fmt.Println("Theta")
	if !nn.buffInitted {
		nn.copyBuffTheta = make([]*mt.CudaMatrix, len(nn.Theta))
		for i := 0; i < len(nn.Theta) - 1; i++ {
			nn.copyBuffTheta[i] = &mt.CudaMatrix{}
			fmt.Println("Theta1", i)
		}
	}
	//fmt.Println("Theta1")

	thetas := make([]*mt.CudaMatrix, len(nn.Theta))
	for i := 0; i < len(nn.Theta); i++ {
		//fmt.Println("Theta2", i, len(nn.Theta[i]), len(nn.Theta[i][0]))
		thetas[i] = mt.GetCudaMatrix(nn.Theta[i])
		//fmt.Println("Theta22", i)
		defer thetas[i].Free()
	}
	//fmt.Println("END Theta")
	m := len(nn.X)
	y := nn.cudaYtrans

	// Feed-forward
	if !nn.buffInitted {
		nn.aBuff = make([]*mt.CudaMatrix, len(nn.Theta) + 1)
	}
	// APPLY IN CUDA
	//log.Println("INIT COST1")
	nn.aBuff[0] = nn.cudaXtransBias
	if !nn.buffInitted {
		nn.zBuff = make([]*mt.CudaMatrix, len(nn.Theta)-1)
	}
	//log.Println("INIT COST2")
	for i := 0; i < len(nn.Theta) - 1; i++ {
		if nn.buffInitted {
			//log.Println("INIT COST22")
			mt.CudaMultTo(thetas[i], nn.aBuff[i], nn.zBuff[i])
			///log.Println("INIT COST23")
			nn.zBuff[i].CudaCopyTo(nn.copyBuffTheta[i]).Sigmoid().AddBiasTopTo(nn.aBuff[i+1])
			//log.Println("INIT COST24")
		} else {
			nn.zBuff[i] = mt.CudaMult(thetas[i], nn.aBuff[i])
			nn.aBuff[i+1] = nn.zBuff[i].CudaCopyTo(nn.copyBuffTheta[i]).Sigmoid().AddBiasTop()
		}
	}
	//log.Println("INIT COST31")
	if nn.buffInitted {
		mt.CudaMultTo(thetas[len(thetas) - 1], nn.aBuff[len(thetas) - 1], nn.aBuff[len(thetas)]).Sigmoid()
	} else {
		nn.aBuff[len(thetas)] = mt.CudaMult(thetas[len(thetas) - 1], nn.aBuff[len(thetas) - 1]).Sigmoid()
	}
	//log.Println("INIT COST41")

	if !nn.buffInitted {
		nn.copyBuff = make([]*mt.CudaMatrix, 4)
		for i := 0; i < 4; i++ {
			nn.copyBuff[i] = &mt.CudaMatrix{}
		}
		nn.cudaMultAllElemsBuff = []*mt.CudaMatrix{
			mt.CudaMultAllElems(y.CudaCopyTo(nn.copyBuff[0]).Neg(), nn.aBuff[len(nn.Theta)].CudaCopyTo(nn.copyBuff[1]).Log()),
			mt.CudaMultAllElems(y.CudaCopyTo(nn.copyBuff[2]).OneMinus(), nn.aBuff[len(nn.Theta)].CudaCopyTo(nn.copyBuff[3]).OneMinus().Log()),
		}
		nn.subJBuff = mt.CudaSub(
			nn.cudaMultAllElemsBuff[0],
			nn.cudaMultAllElemsBuff[1])
	} else {
		mt.CudaMultAllElemsTo(y.CudaCopyTo(nn.copyBuff[0]).Neg(), nn.aBuff[len(nn.Theta)].CudaCopyTo(nn.copyBuff[1]).Log(), nn.cudaMultAllElemsBuff[0])
		//log.Println("INIT COST42")
		mt.CudaMultAllElemsTo(y.CudaCopyTo(nn.copyBuff[2]).OneMinus(), nn.aBuff[len(nn.Theta)].CudaCopyTo(nn.copyBuff[3]).OneMinus().Log(), nn.cudaMultAllElemsBuff[1])
		//log.Println("INIT COST43")
		mt.CudaSubTo(
			nn.cudaMultAllElemsBuff[0],
			nn.cudaMultAllElemsBuff[1],
			nn.subJBuff)
		//log.Println("INIT COST44")
	}
	j = nn.subJBuff.SumAll() / float32(m)

	fmt.Println("J:", j)

	if !nn.buffInitted {
		nn.removeBiasThetaBuff = make([]*mt.CudaMatrix, len(nn.Theta))
	}
	for i, theta := range thetas {
		if !nn.buffInitted {
			nn.removeBiasThetaBuff[i] = &mt.CudaMatrix{}
		}
		j += theta.RemoveBiasTo(nn.removeBiasThetaBuff[i]).PowTwo().SumAll() * (lambda / (2*float32(m)))
	}
	//log.Println("INIT COST6")

	if !nn.buffInitted {
		nn.dBuff = make([]*mt.CudaMatrix, len(nn.Theta))
		nn.dBuffMult = make([]*mt.CudaMatrix, len(nn.Theta))
		nn.dBuff[len(nn.Theta)-1] = mt.CudaSub(nn.aBuff[len(thetas)], y)
		nn.biasTopThetaBuff = make([]*mt.CudaMatrix, len(nn.Theta)-1)
		nn.thetaTransBuff = make([]*mt.CudaMatrix, len(nn.Theta)-1)
	} else {
		mt.CudaSubTo(nn.aBuff[len(thetas)], y, nn.dBuff[len(nn.Theta)-1])
	}
	for i := len(nn.Theta)-2; i >= 0; i-- {
		if !nn.buffInitted {
			nn.copyBuff[i] = &mt.CudaMatrix{}
			nn.dBuff[i] = &mt.CudaMatrix{}
			nn.thetaTransBuff[i] = &mt.CudaMatrix{}
			nn.dBuffMult[i] = mt.CudaMult(thetas[i + 1].TransTo(nn.thetaTransBuff[i]), nn.dBuff[i+1])
			nn.dBuff[i] = mt.CudaMultAllElems(
				nn.dBuffMult[i],
				nn.zBuff[i].SigmoidGradient().AddBiasTopTo(nn.dBuff[i]))
		} else {
			mt.CudaMultTo(thetas[i + 1].TransTo(nn.thetaTransBuff[i]), nn.dBuff[i+1], nn.dBuffMult[i])
			mt.CudaMultAllElemsTo(
				nn.dBuffMult[i],
				nn.zBuff[i].SigmoidGradient().AddBiasTopTo(nn.dBuff[i]),
				nn.dBuff[i])
		}
	}
	//log.Println("INIT COST7")

	grad = make([][][]float32, len(thetas))
	if !nn.buffInitted {
		nn.gradBuff = make([]*mt.CudaMatrix, len(thetas))
		nn.gradRemoveBiasBuff = make([]*mt.CudaMatrix, len(thetas))
		nn.gradBuff[len(thetas)-1] = mt.CudaMultTrans(nn.dBuff[len(thetas)-1].MultBy(1/float32(m)), nn.aBuff[len(thetas)-1])
	} else {
		mt.CudaMultTransTo(nn.dBuff[len(thetas)-1].MultBy(1/float32(m)), nn.aBuff[len(thetas)-1], nn.gradBuff[len(thetas)-1])
	}
	//log.Println("INIT COST8")
	grad[len(thetas)-1] = nn.gradBuff[len(thetas)-1].GetMatrixFromCuda()
	for i := len(nn.Theta)-2; i >= 0; i-- {
		if !nn.buffInitted {
			nn.gradRemoveBiasBuff[i] = nn.dBuff[i].RemoveBiasTop()
			nn.gradBuff[i] = mt.CudaMultTrans(nn.gradRemoveBiasBuff[i].MultBy(1/float32(m)), nn.aBuff[i])
		} else {
			mt.CudaMultTransTo(nn.dBuff[i].RemoveBiasTopTo(nn.gradRemoveBiasBuff[i]).MultBy(1/float32(m)), nn.aBuff[i], nn.gradBuff[i])
		}
		grad[i] = nn.gradBuff[i].GetMatrixFromCuda()
	}
	//log.Println("END COST")

	nn.buffInitted = true

	//fmt.Println("SUM:", mt.SumAll([][]float32{grad[1][0]}))

	return
}

// GetPerformance Returns the performance of the neural network with the current
// set of samples. The performance is calculated as: matches / total_samples
func (nn *NeuralNet) GetPerformance(verbose bool) (cost float32, performance float32) {
	matches := float32(0.0)
	for i := 0; i < len(nn.X); i++ {
		match := true
		prediction := nn.Hipotesis(nn.X[i])

		for i := 0; i < len(prediction); i++ {
			if prediction[i] > 0.5 {
				prediction[i] = 1
			} else {
				prediction[i] = 0
			}
		}

	checkHip:
		for h := 0; h < len(prediction); h++ {
			if nn.Y[i][h] != prediction[h] {
				match = false
				break checkHip
			}
		}

		if match {
			matches++
		}
	}

	cost, _, _ = nn.CostFunction(0, false)
	performance = matches / float32(len(nn.Y))

	return
}

// Hipotesis returns the hipotesis calculation for the sample "x" using the
// thetas of nn.Theta
func (nn *NeuralNet) Hipotesis(x []float32) (result []float32) {
	aux := [][]float32{x}

	for _, theta := range nn.Theta {
		aux = mt.Apply(mt.Mult(addBias(aux), mt.Trans(theta)), sigmoid)
	}
	mt.FreeMem()

	return aux[0]
}

// InitializeThetas Random inizialization of the thetas to break the simetry.
// The slice "layerSizes" will contain on each element, the size of the layer to
// be initialized, the first layer is the input one, and last layer will
// correspond to the output layer
func (nn *NeuralNet) InitializeThetas(layerSizes []int) {
	rand.Seed(int64(time.Now().Nanosecond()))
	epsilon := float32(math.Sqrt(6) / math.Sqrt(float64(layerSizes[0]+layerSizes[len(layerSizes)-1])))

	nn.Theta = make([][][]float32, len(layerSizes)-1)

	for l := 1; l < len(layerSizes); l++ {
		nn.Theta[l-1] = make([][]float32, layerSizes[l])
		for n := 0; n < layerSizes[l]; n++ {
			nn.Theta[l-1][n] = make([]float32, layerSizes[l-1]+1)
			for i := 0; i < layerSizes[l-1]+1; i++ {
				if rand.Float64() > 0.5 {
					nn.Theta[l-1][n][i] = (rand.Float32() * epsilon)
				} else {
					nn.Theta[l-1][n][i] = 0 - (rand.Float32() * epsilon)
				}
			}
		}
	}

	return
}

// MinimizeCost This metod splits the samples contained in the NeuralNet instance
// in three sets (60%, 20%, 20%): training, cross validation and test. In order
// to calculate the optimal theta, after try with different lambda values on the
// training set and compare the performance obtained with the cross validation
// set to get the lambda with a better performance in the cross validation set.
// After calculate the best lambda, merges the training and cross validation
// sets and trains the neural network with the 80% of the samples.
// The data can be shuffled in order to obtain a better distribution before
// divide it in groups
func (nn *NeuralNet) MinimizeCost(maxIters int, suffleData bool, verbose bool) (finalCost float32, performance float32, trainingData *NeuralNet, testData *NeuralNet) {

	lambdas := []float32{0.0, 0.001, 0.003, 0.01, 0.03, 0.1, 0.3, 1, 3, 10, 30, 100, 300}

	if suffleData {
		nn = nn.shuffle()
	}

	// Get the 60% of the nn as training nn, 20% as cross validation, and
	// the remaining 20% as test nn
	training := int64(float32(len(nn.X)) * 0.6)
	cv := int64(float32(len(nn.X)) * 0.8)

	trainingData = &NeuralNet{
		X:     nn.X[:training],
		Y:     nn.Y[:training],
		Theta: nn.Theta,
	}

	cvData := &NeuralNet{
		X:     nn.X[training:cv],
		Y:     nn.Y[training:cv],
		Theta: nn.Theta,
	}
	testData = &NeuralNet{
		X:     nn.X[cv:],
		Y:     nn.Y[cv:],
		Theta: nn.Theta,
	}

	// Launch a process for each lambda in order to obtain the one with best
	// performance
	bestJ := float32(math.Inf(1))
	bestLambda := float32(0.0)
	initTheta := copyTheta(trainingData.Theta)

	for _, posLambda := range lambdas {
		if verbose {
			log.Println("Checking Lambda:", posLambda)
		}
		trainingData.Theta = copyTheta(initTheta)
		Fmincg(trainingData, float64(posLambda), 3, verbose)
		cvData.Theta = trainingData.Theta

		j, _, _ := cvData.CostFunction(posLambda, false)

		if bestJ > j {
			bestJ = j
			bestLambda = posLambda
		}
	}

	// Include the cross validation cases into the training for the final train
	trainingData.X = append(trainingData.X, cvData.X...)
	trainingData.Y = append(trainingData.Y, cvData.Y...)

	if verbose {
		log.Println("Lambda:", bestLambda)
		log.Println("Training with the 80% of the samples...")
	}
	Fmincg(trainingData, float64(bestLambda), maxIters, verbose)

	testData.Theta = trainingData.Theta
	nn.Theta = trainingData.Theta

	finalCost, performance = testData.GetPerformance(verbose)

	return
}

// NewNeuralNetFromCsv Loads the informaton contained in the specified file
// paths and returns a NeuralNet instance.
// Each input file should contain a row by sample, and the values separated by a
// single space.
// To load the thetas specify on thetaSrc the file paths that contains each of
// the layer values. The order of this paths will represent the order of the
// layers.
// In case of need only to load the theta paramateres, specify a empty string on
// the xSrc and ySrc parameters.
func NewNeuralNetFromCsv(xSrc string, ySrc string, thetaSrc []string) (result *NeuralNet) {
	result = new(NeuralNet)

	if xSrc != "" {
		// Parse the X params
		strInfo, err := ioutil.ReadFile(xSrc)
		if err != nil {
			panic(err)
		}

		trainingData := strings.Split(string(strInfo), "\n")
		for _, line := range trainingData {
			if line == "" {
				break
			}

			var values []float32
			for _, value := range strings.Split(line, " ") {
				floatVal, err := strconv.ParseFloat(value, 32)
				if err != nil {
					panic(err)
				}
				values = append(values, float32(floatVal))
			}
			result.X = append(result.X, values)
		}
	}

	if ySrc != "" {
		// Parse the Y params
		strInfo, err := ioutil.ReadFile(ySrc)
		if err != nil {
			panic(err)
		}

		trainingData := strings.Split(string(strInfo), "\n")
		for _, line := range trainingData {
			if line == "" {
				break
			}

			var values []float32
			for _, value := range strings.Split(line, " ") {
				floatVal, err := strconv.ParseFloat(value, 32)
				if err != nil {
					panic(err)
				}
				values = append(values, float32(floatVal))
			}
			result.Y = append(result.Y, values)
		}
	}

	// Parse the Theta params
	for _, thetaNSrc := range thetaSrc {
		strInfo, err := ioutil.ReadFile(thetaNSrc)
		if err != nil {
			panic(err)
		}

		trainingData := strings.Split(string(strInfo), "\n")
		theta := [][]float32{}
		for _, line := range trainingData {
			if line == "" {
				break
			}

			var values []float32
			for _, value := range strings.Split(line, " ") {
				floatVal, err := strconv.ParseFloat(value, 32)
				if err != nil {
					panic(err)
				}
				values = append(values, float32(floatVal))
			}
			theta = append(theta, values)
		}

		result.Theta = append(result.Theta, theta)
	}

	return
}

// SaveThetas Store all the current theta values of the instance in the
// "targetDir" directory.
// This method will create a file for each layer of theta called theta_X.txt
// where X is the layer contained on the file.
func (nn *NeuralNet) SaveThetas(targetDir string) (files []string) {
	fileCont := make([]string, len(nn.Theta))
	for i := 0; i < len(nn.Theta); i++ {
		for j := 0; j < len(nn.Theta[i]); j++ {
			s := []string{}
			for k := 0; k < len(nn.Theta[i][j]); k++ {
				s = append(s, strconv.FormatFloat(float64(nn.Theta[i][j][k]), 'e', -1, 32))
			}

			fileCont[i] += strings.Join(s, " ") + "\n"
		}
	}

	files = make([]string, len(nn.Theta))
	for i := 0; i < len(nn.Theta); i++ {
		files[i] = fmt.Sprintf("%s/theta_%d.txt", targetDir, i)
		ioutil.WriteFile(
			files[i],
			[]byte(fileCont[i]),
			0644)
	}

	return
}

// addBias Returns a copy of the "m" two dim slice with a one added at the
// beginning of each row
func addBias(m [][]float32) (result [][]float32) {
	result = make([][]float32, len(m))

	for i := 0; i < len(m); i++ {
		result[i] = append([]float32{1}, m[i]...)
	}

	return
}

// copyTheta Returns a copy of the "theta" two dim slice allocated in a separate
// memory space
func copyTheta(theta [][][]float32) (copyTheta [][][]float32) {
	copyTheta = make([][][]float32, len(theta))
	for i := 0; i < len(theta); i++ {
		copyTheta[i] = make([][]float32, len(theta[i]))
		for j := 0; j < len(theta[i]); j++ {
			copyTheta[i][j] = make([]float32, len(theta[i][j]))
			for k := 0; k < len(theta[i][j]); k++ {
				copyTheta[i][j][k] = theta[i][j][k]
			}
		}
	}

	return
}

func (nn *NeuralNet) getTheta() [][][]float32 {
	return nn.Theta
}

// removeBias Returns a copy of the given two dim slice without the firs element
// of each row
func removeBias(x [][]float32) (result [][]float32) {
	result = make([][]float32, len(x))
	for i := 0; i < len(x); i++ {
		result[i] = append([]float32{0}, x[i][1:]...)
	}

	return
}

// rollThetasGrad returns a 1 x n matrix with the thetas concatenated
func (nn *NeuralNet) rollThetasGrad(x [][][]float32) [][]float32 {
	result := []float32{}
	for i := 0; i < len(x); i++ {
		for j := 0; j < len(x[i][0]); j++ {
			for k := 0; k < len(x[i]); k++ {
				result = append(result, x[i][k][j])
			}
		}
	}

	return [][]float32{result}
}

func (nn *NeuralNet) setTheta(t [][][]float32) {
	nn.Theta = t
}

// shuffle redistribute randomly all the X and Y rows of the instance
func (nn *NeuralNet) shuffle() (shuffledData *NeuralNet) {
	aux := make([][]float32, len(nn.X))

	copy(aux, nn.X)

	for i := 0; i < len(aux); i++ {
		aux[i] = append(aux[i], nn.Y[i]...)
	}

	dest := make([][]float32, len(aux))
	rand.Seed(int64(time.Now().Nanosecond()))

	for i, v := range rand.Perm(len(aux)) {
		dest[v] = aux[i]
	}

	shuffledData = &NeuralNet{
		X: make([][]float32, len(nn.X)),
		Y: make([][]float32, len(nn.Y)),
	}
	for i := 0; i < len(dest); i++ {
		shuffledData.Y[i] = dest[i][len(dest[i])-len(nn.Y[0]):]
		shuffledData.X[i] = dest[i][:len(dest[i])-len(nn.Y[0])]
	}

	shuffledData.Theta = nn.Theta

	return
}

func sigmoidGradient(x float32) float32 {
	return sigmoid(x) * (1 - sigmoid(x))
}

// unrollThetasGrad Returns the 1 x n matrix as the multilayer theta way
func (nn *NeuralNet) unrollThetasGrad(x [][]float32) (r [][][]float32) {
	pos := 0
	r = make([][][]float32, len(nn.Theta))
	for i := 0; i < len(nn.Theta); i++ {
		r[i] = make([][]float32, len(nn.Theta[i]))
		for j := 0; j < len(nn.Theta[i]); j++ {
			r[i][j] = make([]float32, len(nn.Theta[i][j]))
		}
		for j := 0; j < len(nn.Theta[i][0]); j++ {
			for k := 0; k < len(nn.Theta[i]); k++ {
				r[i][k][j] = x[0][pos]
				pos++
			}
		}
	}

	return
}
